//go:build integration

package incus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pickPool returns the dir pool if present, else the first listed pool. Volume
// CRUD needs a real pool; the test skips if none exist.
func pickPool(t *testing.T, b *incusBackend, ctx context.Context) backend.StoragePool {
	t.Helper()
	pools, err := b.ListStoragePools(ctx)
	require.NoError(t, err)
	if len(pools) == 0 {
		t.Skip("no storage pools on this host")
	}
	for _, p := range pools {
		if p.Driver == "dir" {
			return p
		}
	}
	return pools[0]
}

func TestVolumeCRUDRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	pool := pickPool(t, b, ctx)
	name := fmt.Sprintf("lxvol%d", time.Now().UnixNano()%100000)
	t.Cleanup(func() { _ = b.DeleteVolume(ctx, pool.Name, name) })

	require.NoError(t, b.CreateVolume(ctx, pool.Name, backend.StorageVolume{
		Name: name, ContentType: "filesystem", Config: map[string]string{"size": "32MiB"},
	}))
	v, err := b.GetVolume(ctx, pool.Name, name)
	require.NoError(t, err)
	assert.Equal(t, "custom", v.Type)

	require.NoError(t, b.DeleteVolume(ctx, pool.Name, name))
	_, err = b.GetVolume(ctx, pool.Name, name)
	require.ErrorIs(t, err, backend.ErrNotFound)
}
