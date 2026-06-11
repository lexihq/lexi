package incus

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToImagesKeepsDistinctImageTypes(t *testing.T) {
	images := []api.Image{
		{
			Aliases:      []api.ImageAlias{{Name: "debian/12"}},
			Architecture: "x86_64",
			Fingerprint:  "container-fingerprint",
			Type:         "container",
		},
		{
			Aliases:      []api.ImageAlias{{Name: "debian/12"}},
			Architecture: "x86_64",
			Fingerprint:  "vm-fingerprint",
			Type:         "virtual-machine",
		},
	}

	got := toImages(images)

	require.Len(t, got, 2)
	assert.Equal(t, "container-fingerprint", got[0].Fingerprint)
	assert.Equal(t, "container", got[0].Type)
	assert.Equal(t, "vm-fingerprint", got[1].Fingerprint)
	assert.Equal(t, "virtual-machine", got[1].Type)
}

func TestListLocalImagesMapsFields(t *testing.T) {
	created := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	b := &incusBackend{srv: &instanceServerStub{
		localImages: []api.Image{
			{
				Fingerprint:  "zfp",
				Aliases:      []api.ImageAlias{{Name: "z/1"}},
				Architecture: "x86_64",
				Type:         "container",
			},
			{
				Fingerprint:  "afp",
				Aliases:      []api.ImageAlias{{Name: "a/1"}, {Name: "a/latest"}},
				Architecture: "aarch64",
				Size:         123,
				Type:         "virtual-machine",
				ImagePut:     api.ImagePut{Properties: map[string]string{"description": "A image"}},
				CreatedAt:    created,
			},
		},
	}}

	got, err := b.ListLocalImages(context.Background())

	require.NoError(t, err)
	require.Len(t, got, 2)
	// Sorted by fingerprint for a stable listing.
	assert.Equal(t, "afp", got[0].Fingerprint)
	assert.Equal(t, []string{"a/1", "a/latest"}, got[0].Aliases)
	assert.Equal(t, "A image", got[0].Description)
	assert.Equal(t, "aarch64", got[0].Arch)
	assert.Equal(t, int64(123), got[0].SizeBytes)
	assert.Equal(t, "virtual-machine", got[0].Type)
	assert.Equal(t, created, got[0].CreatedAt)
	assert.Equal(t, "zfp", got[1].Fingerprint)
}

func TestPublishImageSendsSourceAndAlias(t *testing.T) {
	srv := &instanceServerStub{
		createImageOp: &operationStub{get: api.Operation{Metadata: map[string]any{"fingerprint": "pubfp"}}},
	}
	b := &incusBackend{srv: srv}

	require.NoError(t, b.PublishImage(context.Background(), "demo", "my-alias"))

	require.NotNil(t, srv.createdImage)
	require.NotNil(t, srv.createdImage.Source)
	assert.Equal(t, "instance", srv.createdImage.Source.Type)
	assert.Equal(t, "demo", srv.createdImage.Source.Name)
	require.NotNil(t, srv.createdAlias)
	assert.Equal(t, "my-alias", srv.createdAlias.Name)
	assert.Equal(t, "pubfp", srv.createdAlias.Target)
}

func TestPublishImageWithoutAliasSkipsAliasCreation(t *testing.T) {
	srv := &instanceServerStub{}
	b := &incusBackend{srv: srv}

	require.NoError(t, b.PublishImage(context.Background(), "demo", ""))

	require.NotNil(t, srv.createdImage)
	assert.Nil(t, srv.createdAlias)
}

func TestPublishImageMissingFingerprintFails(t *testing.T) {
	srv := &instanceServerStub{createImageOp: &operationStub{}}
	b := &incusBackend{srv: srv}

	err := b.PublishImage(context.Background(), "demo", "my-alias")

	require.Error(t, err)
	assert.Nil(t, srv.createdAlias)
}

func TestPublishImageRollsBackOnAliasFailure(t *testing.T) {
	srv := &instanceServerStub{
		createImageOp: &operationStub{get: api.Operation{Metadata: map[string]any{"fingerprint": "pubfp"}}},
		aliasErr:      errors.New("already exists"),
	}
	b := &incusBackend{srv: srv}

	err := b.PublishImage(context.Background(), "demo", "dup")

	require.Error(t, err)
	// The just-published image must not be left orphaned when aliasing fails.
	assert.Equal(t, "pubfp", srv.deletedImage)
}

func TestCopyImageFromResolvesAliasAndCopies(t *testing.T) {
	op := &remoteOperationStub{started: make(chan struct{}), cancelled: make(chan struct{})}
	close(op.cancelled) // success path: Wait returns immediately
	srv := &instanceServerStub{imageCopyOp: op}
	b := &incusBackend{srv: srv}
	remote := &imageServerStub{
		alias: &api.ImageAliasesEntry{Name: "debian/12", ImageAliasesEntryPut: api.ImageAliasesEntryPut{Target: "remotefp"}},
		image: &api.Image{Fingerprint: "remotefp"},
	}

	require.NoError(t, b.copyImageFrom(context.Background(), remote, "debian/12"))

	assert.Equal(t, remote, srv.imageCopySrc)
	require.NotNil(t, srv.imageCopied)
	assert.Equal(t, "remotefp", srv.imageCopied.Fingerprint)
	require.NotNil(t, srv.imageCopyArgs)
	assert.True(t, srv.imageCopyArgs.CopyAliases)
}

func TestDeleteImagePassesFingerprint(t *testing.T) {
	srv := &instanceServerStub{}
	b := &incusBackend{srv: srv}

	require.NoError(t, b.DeleteImage(context.Background(), "fp123"))

	assert.Equal(t, "fp123", srv.deletedImage)
}

func TestAddImageAliasPassesNameAndTarget(t *testing.T) {
	srv := &instanceServerStub{}
	b := &incusBackend{srv: srv}

	require.NoError(t, b.AddImageAlias(context.Background(), "fp123", "extra"))

	require.NotNil(t, srv.createdAlias)
	assert.Equal(t, "extra", srv.createdAlias.Name)
	assert.Equal(t, "fp123", srv.createdAlias.Target)
}

func TestRemoveImageAliasPassesName(t *testing.T) {
	srv := &instanceServerStub{}
	b := &incusBackend{srv: srv}

	require.NoError(t, b.RemoveImageAlias(context.Background(), "extra"))

	assert.Equal(t, "extra", srv.deletedAlias)
}

func TestExportImageUnifiedStreamsMeta(t *testing.T) {
	s := &instanceServerStub{
		image:         &api.Image{Type: "container"},
		imageFileMeta: []byte("meta-tarball-bytes"),
	}
	b := &incusBackend{srv: s}

	filename, rc, err := b.ExportImage(t.Context(), "fp")
	require.NoError(t, err)
	blob, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())

	assert.Equal(t, "meta.tar.gz", filename, "the daemon-reported payload name carries the real extension")
	assert.Equal(t, "meta-tarball-bytes", string(blob))
	require.NotNil(t, s.imageFileReq.Canceler, "image download should be cancelable")
}

func TestExportImageSplitBuildsZip(t *testing.T) {
	s := &instanceServerStub{
		image:           &api.Image{Type: "virtual-machine"},
		imageFileMeta:   []byte("meta-bytes"),
		imageFileRootfs: []byte("rootfs-bytes"),
	}
	b := &incusBackend{srv: s}

	filename, rc, err := b.ExportImage(t.Context(), "fp")
	require.NoError(t, err)
	blob, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())

	assert.Equal(t, "fp.zip", filename)
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	require.NoError(t, err)
	require.Len(t, zr.File, 2)
	want := map[string]string{"metadata": "meta-bytes", "rootfs.img": "rootfs-bytes"}
	for _, zf := range zr.File {
		assert.Equal(t, zip.Store, zf.Method, zf.Name)
		f, err := zf.Open()
		require.NoError(t, err)
		got, err := io.ReadAll(f)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		assert.Equal(t, want[zf.Name], string(got), zf.Name)
	}
}

func TestExportImageGhostFingerprintFailsBeforeDownload(t *testing.T) {
	b := &incusBackend{srv: &instanceServerStub{}}
	_, _, err := b.ExportImage(t.Context(), "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestImportImageSplitZipUploadsBothParts(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct{ name, payload string }{
		{"metadata", "meta-bytes"}, {"rootfs.img", "rootfs-bytes"},
	} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Store})
		require.NoError(t, err)
		_, err = w.Write([]byte(e.payload))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())

	s := &instanceServerStub{createImageOp: &operationStub{}}
	b := &incusBackend{srv: s}
	require.NoError(t, b.ImportImage(t.Context(), bytes.NewReader(buf.Bytes()), ""))

	require.NotNil(t, s.createImageArgs)
	assert.Equal(t, "virtual-machine", s.createImageArgs.Type, "rootfs.img entry carries the VM type")
	assert.Equal(t, "meta-bytes", string(s.createImageMeta))
	assert.Equal(t, "rootfs-bytes", string(s.createImageRootfs))
}

func TestImportImageRejectsForeignZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "evil", Method: zip.Store})
	require.NoError(t, err)
	_, err = w.Write([]byte("x"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	s := &instanceServerStub{createImageOp: &operationStub{}}
	b := &incusBackend{srv: s}
	err = b.ImportImage(t.Context(), bytes.NewReader(buf.Bytes()), "")
	require.ErrorIs(t, err, backend.ErrInvalid)
	assert.Nil(t, s.createImageArgs, "foreign zips must be rejected before any upload")
}
