//go:build integration

package incus

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
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
	require.NotEmpty(t, v.Version)

	// Versioned update: description + resize via the size key; stale etag
	// conflicts.
	require.NoError(t, b.UpdateVolume(ctx, pool.Name, name, "edited", map[string]string{"size": "64MiB"}, v.Version))
	got, err := b.GetVolume(ctx, pool.Name, name)
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, "64MiB", got.Config["size"])
	require.ErrorIs(t, b.UpdateVolume(ctx, pool.Name, name, "stale", nil, v.Version), backend.ErrConflict)

	// Rename moves the volume; the old name is gone and a collision conflicts.
	renamed := name + "r"
	t.Cleanup(func() { _ = b.DeleteVolume(ctx, pool.Name, renamed) })
	require.NoError(t, b.RenameVolume(ctx, pool.Name, name, renamed))
	_, err = b.GetVolume(ctx, pool.Name, name)
	require.ErrorIs(t, err, backend.ErrNotFound)
	require.NoError(t, b.CreateVolume(ctx, pool.Name, backend.StorageVolume{Name: name, ContentType: "filesystem"}))
	require.ErrorIs(t, b.RenameVolume(ctx, pool.Name, name, renamed), backend.ErrConflict)

	require.NoError(t, b.DeleteVolume(ctx, pool.Name, name))
	require.NoError(t, b.DeleteVolume(ctx, pool.Name, renamed))
	_, err = b.GetVolume(ctx, pool.Name, renamed)
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
	name := uniqueName("lexi-pool")

	require.NoError(t, b.CreateStoragePool(ctx, backend.StoragePool{
		Name: name, Driver: "dir", Description: "lexi integration",
	}))
	t.Cleanup(func() {
		if err := b.DeleteStoragePool(context.Background(), name); err != nil {
			t.Logf("cleanup pool %q: %v", name, err)
		}
	})

	p, err := b.GetStoragePool(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "dir", p.Driver)
	assert.Equal(t, "lexi integration", p.Description)
	require.NotEmpty(t, p.Version)

	// Versioned update round-trips description + a config key; a stale etag
	// conflicts. Like the UI editor, start from the fetched config — the daemon
	// materializes immutable keys (source) at create time and rejects a PUT
	// that drops them ("Pool source cannot be changed when not in pending
	// state").
	cfg := maps.Clone(p.Config)
	if cfg == nil {
		cfg = map[string]string{}
	}
	cfg["rsync.bwlimit"] = "10MiB"
	require.NoError(t, b.UpdateStoragePool(ctx, name, "edited", cfg, p.Version))
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

// TestVolumeExportImportRoundTrip exports a custom volume to a buffer (a gzip
// backup tarball), deletes the volume, and re-imports it under a new name.
func TestVolumeExportImportRoundTrip(t *testing.T) {
	b := newBackend(t)
	if !b.Capabilities(context.Background()).VolumeBackups {
		t.Skip("daemon lacks the custom_volume_backup/backup_override_name extensions")
	}
	// Deadline so a daemon death mid-backup fails in minutes (the instance
	// export test precedent).
	ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelCtx()
	pool := pickPool(t, b, ctx)
	name := fmt.Sprintf("lxvb%d", time.Now().UnixNano()%100000)
	restored := name + "r"
	t.Cleanup(func() {
		_ = b.DeleteVolume(ctx, pool.Name, name)
		_ = b.DeleteVolume(ctx, pool.Name, restored)
	})

	require.NoError(t, b.CreateVolume(ctx, pool.Name, backend.StorageVolume{
		Name: name, ContentType: "filesystem", Config: map[string]string{"size": "32MiB"},
	}))

	var buf bytes.Buffer
	require.NoError(t, b.ExportVolume(ctx, pool.Name, name, &buf))
	assert.Greater(t, buf.Len(), 2)
	assert.Equal(t, []byte{0x1f, 0x8b}, buf.Bytes()[:2], "export should be a gzip stream")

	// Ghost volumes are not found.
	require.ErrorIs(t, b.ExportVolume(ctx, pool.Name, "lx-ghost", &bytes.Buffer{}), backend.ErrNotFound)

	// Importing over an existing name conflicts; a fresh name restores.
	require.ErrorIs(t, b.ImportVolume(ctx, pool.Name, name, bytes.NewReader(buf.Bytes())), backend.ErrConflict)
	require.NoError(t, b.ImportVolume(ctx, pool.Name, restored, bytes.NewReader(buf.Bytes())))

	got, err := b.GetVolume(ctx, pool.Name, restored)
	require.NoError(t, err)
	assert.Equal(t, "32MiB", got.Config["size"], "config survives the round-trip")
}

func TestCreateVolumeFromISOIntegration(t *testing.T) {
	b := newBackend(t)
	if !b.Capabilities(context.Background()).ISOVolumes {
		t.Skip("daemon lacks the custom_volume_iso extension")
	}
	ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelCtx()
	pool := pickPool(t, b, ctx)
	name := fmt.Sprintf("lxiso%d", time.Now().UnixNano()%100000)
	t.Cleanup(func() { _ = b.DeleteVolume(ctx, pool.Name, name) })

	iso := bytes.Repeat([]byte("lexi-iso-test-payload\n"), 64)
	require.NoError(t, b.CreateVolumeFromISO(ctx, pool.Name, name, bytes.NewReader(iso)))

	got, err := b.GetVolume(ctx, pool.Name, name)
	require.NoError(t, err)
	assert.Equal(t, "custom", got.Type)
	assert.Equal(t, "iso", got.ContentType)

	// The name is the volume's identity: a second upload conflicts.
	require.ErrorIs(t, b.CreateVolumeFromISO(ctx, pool.Name, name, bytes.NewReader(iso)), backend.ErrConflict)
	// Invalid names are rejected before any upload.
	require.ErrorIs(t, b.CreateVolumeFromISO(ctx, pool.Name, "bad name", bytes.NewReader(iso)), backend.ErrInvalid)
}
