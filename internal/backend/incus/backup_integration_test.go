//go:build integration

package incus

import (
	"bytes"
	"context"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExportInstanceProducesTarball exports a throwaway instance to a temp file
// and asserts it is a non-empty gzip stream (the requested compression).
func TestExportInstanceProducesTarball(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("export")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	var buf bytes.Buffer
	require.NoError(t, b.ExportInstance(ctx, name, &buf))

	require.Positive(t, buf.Len(), "export should produce a non-empty backup")
	assert.Equal(t, []byte{0x1f, 0x8b}, buf.Bytes()[:2], "backup should be a gzip stream")
}

// TestExportImportRoundTrip exports a throwaway instance and imports the
// resulting tarball back under a new name, asserting the clone is listed.
func TestExportImportRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	src := uniqueName("exp-src")
	dst := uniqueName("exp-dst")
	t.Cleanup(func() {
		cleanupInstance(t, b, dst)
		cleanupInstance(t, b, src)
	})

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: src, Image: testImage}))

	var buf bytes.Buffer
	require.NoError(t, b.ExportInstance(ctx, src, &buf))
	require.NoError(t, b.ImportInstance(ctx, dst, &buf))

	list, err := b.ListInstances(ctx)
	require.NoError(t, err)
	assert.True(t, listed(list, dst), "imported instance %q should be listed", dst)
}
