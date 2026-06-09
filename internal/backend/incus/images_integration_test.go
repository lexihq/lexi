//go:build integration

package incus

import (
	"context"
	"testing"

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
