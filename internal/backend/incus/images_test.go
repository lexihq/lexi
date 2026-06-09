package incus

import (
	"context"
	"testing"
	"time"

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
