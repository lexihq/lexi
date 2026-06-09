//go:build integration

package incus

import (
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
