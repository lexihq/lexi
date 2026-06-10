//go:build integration

package incus

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/adam/lxcon/internal/backend"
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
	require.NoError(t, b.UpdateImage(ctx, fingerprint, "edited by lxcon", true))
	imgs, err = b.ListLocalImages(ctx)
	require.NoError(t, err)
	idx := slices.IndexFunc(imgs, func(i backend.LocalImage) bool { return i.Fingerprint == fingerprint })
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, "edited by lxcon", imgs[idx].Description)
	assert.True(t, imgs[idx].Public)

	// Export the unified tarball, delete the original, re-import under a new
	// alias (same fingerprint content).
	var buf bytes.Buffer
	require.NoError(t, b.ExportImage(ctx, fingerprint, &buf))
	require.Greater(t, buf.Len(), 0)
	require.NoError(t, b.DeleteImage(ctx, fingerprint))

	restored := alias + "-restored"
	require.NoError(t, b.ImportImage(ctx, &buf, restored))
	t.Cleanup(func() { _ = b.DeleteImage(ctx, fingerprint) })
	imgs, err = b.ListLocalImages(ctx)
	require.NoError(t, err)
	idx = slices.IndexFunc(imgs, func(i backend.LocalImage) bool { return slices.Contains(i.Aliases, restored) })
	require.GreaterOrEqual(t, idx, 0, "re-imported image missing")
	assert.Equal(t, fingerprint, imgs[idx].Fingerprint, "import preserves the image identity")
}
