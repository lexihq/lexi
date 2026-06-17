//go:build integration

package incus

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListImages (read path) asserts the full simplestreams catalog resolves —
// far larger than the v1 curated set — with filterable fields populated.
func TestListImages(t *testing.T) {
	imgs, err := newBackend(t).ListImages(context.Background())
	require.NoError(t, err)
	require.Greater(t, len(imgs), 10, "expected the full catalog, not a curated subset")

	distros := map[string]bool{}
	var foundTest bool
	for _, i := range imgs {
		distros[i.Distribution] = true
		if i.Alias == testImage {
			foundTest = true
		}
	}
	assert.True(t, foundTest, "test image %q should be in the catalog", testImage)
	assert.Greater(t, len(distros), 1, "catalog should span multiple distributions")
	t.Logf("resolved %d images across %d distributions", len(imgs), len(distros))
}

// TestPublishImageRoundTrip publishes an image from a stopped instance, manages
// its aliases, and deletes it. Copy from the remote is deliberately not
// integration-tested (external network dependency) — it's covered by the stub
// and e2e suites.
func TestPublishImageRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()

	name := uniqueName("img")
	t.Cleanup(func() { cleanupInstance(t, b, name) })
	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	alias := name + "-img"
	require.NoError(t, b.PublishImage(ctx, name, alias))

	imgs, err := b.ListLocalImages(ctx)
	require.NoError(t, err)
	var fingerprint string
	for _, img := range imgs {
		if slices.Contains(img.Aliases, alias) {
			fingerprint = img.Fingerprint
		}
	}
	require.NotEmpty(t, fingerprint, "published image with alias %q not in local list", alias)
	t.Cleanup(func() { _ = b.DeleteImage(ctx, fingerprint) }) // belt-and-braces if asserts below fail

	extra := alias + "-extra"
	require.NoError(t, b.AddImageAlias(ctx, fingerprint, extra))
	require.NoError(t, b.RemoveImageAlias(ctx, extra))

	require.NoError(t, b.DeleteImage(ctx, fingerprint))
	imgs, err = b.ListLocalImages(ctx)
	require.NoError(t, err)
	for _, img := range imgs {
		assert.NotEqual(t, fingerprint, img.Fingerprint, "deleted image still listed")
	}
}

// TestImageUpdateExportImportRoundTrip publishes an instance (a unified image),
// edits its description/public flag, exports the tarball, deletes the original,
// and re-imports it with a fresh alias.
func TestImageUpdateExportImportRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()

	name := uniqueName("imgrt")
	t.Cleanup(func() { cleanupInstance(t, b, name) })
	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	alias := name + "-img"
	require.NoError(t, b.PublishImage(ctx, name, alias))
	imgs, err := b.ListLocalImages(ctx)
	require.NoError(t, err)
	var fingerprint string
	for _, img := range imgs {
		if slices.Contains(img.Aliases, alias) {
			fingerprint = img.Fingerprint
		}
	}
	require.NotEmpty(t, fingerprint)
	t.Cleanup(func() { _ = b.DeleteImage(ctx, fingerprint) })

	// Update description + public and read both back.
	require.NoError(t, b.UpdateImage(ctx, fingerprint, backend.ImageEdit{Description: "edited by lexi", Public: true}))
	imgs, err = b.ListLocalImages(ctx)
	require.NoError(t, err)
	idx := slices.IndexFunc(imgs, func(i backend.LocalImage) bool { return i.Fingerprint == fingerprint })
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, "edited by lexi", imgs[idx].Description)
	assert.True(t, imgs[idx].Public)

	// Export the unified tarball, delete the original, re-import under a new
	// alias (same fingerprint content).
	filename, rc, err := b.ExportImage(ctx, fingerprint)
	require.NoError(t, err)
	blob, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.True(t, strings.HasPrefix(filename, fingerprint), "unified exports carry the daemon-reported name, got %q", filename)
	assert.False(t, strings.HasSuffix(filename, ".zip"), "published container images export unified")
	require.NotEmpty(t, blob)
	require.NoError(t, b.DeleteImage(ctx, fingerprint))

	restored := alias + "-restored"
	require.NoError(t, b.ImportImage(ctx, bytes.NewReader(blob), restored))
	t.Cleanup(func() { _ = b.DeleteImage(ctx, fingerprint) })
	imgs, err = b.ListLocalImages(ctx)
	require.NoError(t, err)
	idx = slices.IndexFunc(imgs, func(i backend.LocalImage) bool { return slices.Contains(i.Aliases, restored) })
	require.GreaterOrEqual(t, idx, 0, "re-imported image missing")
	assert.Equal(t, fingerprint, imgs[idx].Fingerprint, "import preserves the image identity")
}

// buildImageMetadata builds a minimal metadata tarball (gzip) the daemon
// accepts: just metadata.yaml with an architecture and creation date.
func buildImageMetadata(t *testing.T, description string) []byte {
	t.Helper()
	yaml := "architecture: aarch64\ncreation_date: " + fmt.Sprint(time.Now().Unix()) +
		"\nproperties:\n  description: " + description + "\n"
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "metadata.yaml", Mode: 0o644, Size: int64(len(yaml))}))
	_, err := tw.Write([]byte(yaml))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// buildSplitImageZip packs metadata + rootfs into the lexi split-image zip.
func buildSplitImageZip(t *testing.T, meta, rootfs []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct {
		name    string
		payload []byte
	}{{"metadata", meta}, {"rootfs.img", rootfs}} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Store})
		require.NoError(t, err)
		_, err = w.Write(e.payload)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// TestSplitImageImportExportRoundTrip handcrafts a split VM image (the daemon
// validates the metadata tarball, not rootfs contents), imports it through
// the zip path, exports it back as a zip with both parts, and re-imports the
// export asserting fingerprint identity.
func TestSplitImageImportExportRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	alias := uniqueName("splitimg")
	meta := buildImageMetadata(t, "lexi split-image integration test")
	rootfs := []byte("lexi-fake-vm-rootfs-blob")

	require.NoError(t, b.ImportImage(ctx, bytes.NewReader(buildSplitImageZip(t, meta, rootfs)), alias))

	imgs, err := b.ListLocalImages(ctx)
	require.NoError(t, err)
	idx := slices.IndexFunc(imgs, func(i backend.LocalImage) bool { return slices.Contains(i.Aliases, alias) })
	require.GreaterOrEqual(t, idx, 0, "imported split image missing")
	fingerprint := imgs[idx].Fingerprint
	t.Cleanup(func() { _ = b.DeleteImage(ctx, fingerprint) })
	assert.Equal(t, "virtual-machine", imgs[idx].Type, "rootfs.img entry imports as a VM image")

	// Export comes back as a split zip with both parts intact.
	filename, rc, err := b.ExportImage(ctx, fingerprint)
	require.NoError(t, err)
	blob, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	require.True(t, strings.HasSuffix(filename, ".zip"), "split exports are zips, got %q", filename)
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	require.NoError(t, err)
	require.Len(t, zr.File, 2)
	for _, zf := range zr.File {
		assert.Positive(t, zf.UncompressedSize64, zf.Name)
	}

	// Delete and re-import the export: same content, same fingerprint.
	require.NoError(t, b.DeleteImage(ctx, fingerprint))
	require.NoError(t, b.ImportImage(ctx, bytes.NewReader(blob), alias+"r"))
	imgs, err = b.ListLocalImages(ctx)
	require.NoError(t, err)
	idx = slices.IndexFunc(imgs, func(i backend.LocalImage) bool { return slices.Contains(i.Aliases, alias+"r") })
	require.GreaterOrEqual(t, idx, 0, "re-imported split image missing")
	assert.Equal(t, fingerprint, imgs[idx].Fingerprint, "split round-trip preserves image identity")
}
