//go:build integration

package incus

import (
	"context"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMetricsReportsUsage starts a throwaway container and reads its live
// metrics back, asserting memory usage is populated once it is running.
func TestMetricsReportsUsage(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("metrics")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage, Start: true}))

	m, err := b.Metrics(ctx, name)
	require.NoError(t, err)
	assert.Greater(t, m.MemoryUsage, int64(0), "running instance should report memory usage")
}
