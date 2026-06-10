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

func TestVolumeSnapshotRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	pool := pickPool(t, b, ctx)
	name := fmt.Sprintf("lxvol%d", time.Now().UnixNano()%100000)
	t.Cleanup(func() { _ = b.DeleteVolume(ctx, pool.Name, name) })

	require.NoError(t, b.CreateVolume(ctx, pool.Name, backend.StorageVolume{
		Name: name, ContentType: "filesystem", Config: map[string]string{"size": "32MiB"},
	}))

	require.NoError(t, b.CreateVolumeSnapshot(ctx, pool.Name, name, "snap0"))
	snaps, err := b.ListVolumeSnapshots(ctx, pool.Name, name)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "snap0", snaps[0].Name)

	require.NoError(t, b.RestoreVolumeSnapshot(ctx, pool.Name, name, "snap0"))

	// Set expiry, then rename: both round-trip via the listing.
	when := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, b.UpdateVolumeSnapshotExpiry(ctx, pool.Name, name, "snap0", when))
	snaps, err = b.ListVolumeSnapshots(ctx, pool.Name, name)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.WithinDuration(t, when, snaps[0].ExpiresAt, time.Second)

	require.NoError(t, b.RenameVolumeSnapshot(ctx, pool.Name, name, "snap0", "snap1"))
	snaps, err = b.ListVolumeSnapshots(ctx, pool.Name, name)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "snap1", snaps[0].Name)
	assert.WithinDuration(t, when, snaps[0].ExpiresAt, time.Second)

	// Clearing expiry with a zero time.
	require.NoError(t, b.UpdateVolumeSnapshotExpiry(ctx, pool.Name, name, "snap1", time.Time{}))
	snaps, err = b.ListVolumeSnapshots(ctx, pool.Name, name)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.True(t, snaps[0].ExpiresAt.IsZero())

	require.NoError(t, b.DeleteVolumeSnapshot(ctx, pool.Name, name, "snap1"))
	snaps, err = b.ListVolumeSnapshots(ctx, pool.Name, name)
	require.NoError(t, err)
	assert.Empty(t, snaps)

	require.NoError(t, b.DeleteVolume(ctx, pool.Name, name))
}

// TestStoragePoolCreateDeleteRoundTrip creates a throwaway dir pool, verifies
// a pool holding a volume refuses deletion, then deletes it once empty.
func TestStoragePoolCreateDeleteRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("lxcon-pool")

	require.NoError(t, b.CreateStoragePool(ctx, backend.StoragePool{
		Name: name, Driver: "dir", Description: "lxcon integration",
	}))
	t.Cleanup(func() {
		if err := b.DeleteStoragePool(context.Background(), name); err != nil {
			t.Logf("cleanup pool %q: %v", name, err)
		}
	})

	p, err := b.GetStoragePool(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "dir", p.Driver)
	assert.Equal(t, "lxcon integration", p.Description)
	require.NotEmpty(t, p.Version)

	// Versioned update round-trips description + a config key; a stale etag
	// conflicts.
	require.NoError(t, b.UpdateStoragePool(ctx, name, "edited", map[string]string{"rsync.bwlimit": "10MiB"}, p.Version))
	got, err := b.GetStoragePool(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, "10MiB", got.Config["rsync.bwlimit"])
	require.ErrorIs(t, b.UpdateStoragePool(ctx, name, "stale", nil, p.Version), backend.ErrConflict)

	// Duplicate create conflicts.
	require.ErrorIs(t, b.CreateStoragePool(ctx, backend.StoragePool{Name: name, Driver: "dir"}), backend.ErrConflict)

	// A pool holding a custom volume refuses deletion.
	require.NoError(t, b.CreateVolume(ctx, name, backend.StorageVolume{Name: "blocker", ContentType: "filesystem"}))
	require.ErrorIs(t, b.DeleteStoragePool(ctx, name), backend.ErrConflict)
	require.NoError(t, b.DeleteVolume(ctx, name, "blocker"))

	require.NoError(t, b.DeleteStoragePool(ctx, name))
	_, err = b.GetStoragePool(ctx, name)
	require.ErrorIs(t, err, backend.ErrNotFound)
}
