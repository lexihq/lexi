package fake

import (
	"strings"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateStoragePoolRoundTrip(t *testing.T) {
	f := New()

	require.NoError(t, f.CreateStoragePool(ctx(), backend.StoragePool{
		Name: "data", Driver: "dir", Description: "scratch", Config: map[string]string{"source": "/tmp/data"},
	}))

	p, err := f.GetStoragePool(ctx(), "data")
	require.NoError(t, err)
	assert.Equal(t, "dir", p.Driver)
	assert.Equal(t, "scratch", p.Description)
	assert.Equal(t, "/tmp/data", p.Config["source"])
	assert.Empty(t, p.UsedBy)
}

func TestCreateStoragePoolDuplicateIsConflict(t *testing.T) {
	f := New()
	err := f.CreateStoragePool(ctx(), backend.StoragePool{Name: "default", Driver: "dir"})
	require.ErrorIs(t, err, backend.ErrConflict)
}

func TestCreateStoragePoolValidatesNameAndDriver(t *testing.T) {
	f := New()
	require.ErrorIs(t, f.CreateStoragePool(ctx(), backend.StoragePool{Driver: "dir"}), backend.ErrInvalid)
	require.ErrorIs(t, f.CreateStoragePool(ctx(), backend.StoragePool{Name: "data"}), backend.ErrInvalid)
	// Incus parity: names with whitespace or slashes and unknown drivers are
	// rejected, so fake-backed tests can't pass with requests production refuses.
	require.ErrorIs(t, f.CreateStoragePool(ctx(), backend.StoragePool{Name: "bad name", Driver: "dir"}), backend.ErrInvalid)
	require.ErrorIs(t, f.CreateStoragePool(ctx(), backend.StoragePool{Name: "bad/name", Driver: "dir"}), backend.ErrInvalid)
	require.ErrorIs(t, f.CreateStoragePool(ctx(), backend.StoragePool{Name: strings.Repeat("x", 65), Driver: "dir"}), backend.ErrInvalid)
	require.ErrorIs(t, f.CreateStoragePool(ctx(), backend.StoragePool{Name: "data", Driver: "bogus"}), backend.ErrInvalid)
}

func TestStoragePoolUsedByListsProfileReferences(t *testing.T) {
	f := New()
	// The seeded default profile's root device targets the default pool.
	p, err := f.GetStoragePool(ctx(), "default")
	require.NoError(t, err)
	assert.Contains(t, p.UsedBy, "/1.0/profiles/default")
}

func TestDeleteStoragePoolCleanPool(t *testing.T) {
	f := New()
	require.NoError(t, f.DeleteStoragePool(ctx(), "zfs0"))
	_, err := f.GetStoragePool(ctx(), "zfs0")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestDeleteStoragePoolReferencedIsConflict(t *testing.T) {
	f := New()
	require.ErrorIs(t, f.DeleteStoragePool(ctx(), "default"), backend.ErrConflict)
}

func TestDeleteStoragePoolWithVolumesIsConflict(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateVolume(ctx(), "zfs0", backend.StorageVolume{Name: "keep"}))
	require.ErrorIs(t, f.DeleteStoragePool(ctx(), "zfs0"), backend.ErrConflict)
}

func TestDeleteStoragePoolGhostIs404(t *testing.T) {
	f := New()
	require.ErrorIs(t, f.DeleteStoragePool(ctx(), "ghost"), backend.ErrNotFound)
}

func TestStoragePoolsAndVolumes(t *testing.T) {
	f := New()

	pools, err := f.ListStoragePools(ctx())
	require.NoError(t, err)
	assert.NotEmpty(t, pools) // seeded default + zfs0

	p, err := f.GetStoragePool(ctx(), "default")
	require.NoError(t, err)
	assert.Equal(t, "dir", p.Driver)

	require.NoError(t, f.CreateVolume(ctx(), "default", backend.StorageVolume{Name: "vol1", ContentType: "filesystem", Config: map[string]string{"size": "1GiB"}}))
	vols, err := f.ListVolumes(ctx(), "default")
	require.NoError(t, err)
	assert.Len(t, vols, 1)

	v, err := f.GetVolume(ctx(), "default", "vol1")
	require.NoError(t, err)
	assert.Equal(t, "custom", v.Type)
	assert.Equal(t, "1GiB", v.Config["size"])

	require.ErrorIs(t, f.CreateVolume(ctx(), "default", backend.StorageVolume{Name: "vol1"}), backend.ErrConflict)
	require.ErrorIs(t, f.CreateVolume(ctx(), "ghost", backend.StorageVolume{Name: "x"}), backend.ErrNotFound)

	require.NoError(t, f.DeleteVolume(ctx(), "default", "vol1"))
	require.ErrorIs(t, f.DeleteVolume(ctx(), "default", "vol1"), backend.ErrNotFound)
	_, err = f.GetStoragePool(ctx(), "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestVolumeSnapshots(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateVolume(ctx(), "default", backend.StorageVolume{Name: "vol1", ContentType: "filesystem"}))

	snaps, err := f.ListVolumeSnapshots(ctx(), "default", "vol1")
	require.NoError(t, err)
	assert.Empty(t, snaps)

	require.NoError(t, f.CreateVolumeSnapshot(ctx(), "default", "vol1", "snap0"))
	snaps, err = f.ListVolumeSnapshots(ctx(), "default", "vol1")
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "snap0", snaps[0].Name)
	assert.False(t, snaps[0].CreatedAt.IsZero())

	require.NoError(t, f.RestoreVolumeSnapshot(ctx(), "default", "vol1", "snap0"))
	require.NoError(t, f.DeleteVolumeSnapshot(ctx(), "default", "vol1", "snap0"))
	snaps, err = f.ListVolumeSnapshots(ctx(), "default", "vol1")
	require.NoError(t, err)
	assert.Empty(t, snaps)

	// Duplicate snapshot conflicts.
	require.NoError(t, f.CreateVolumeSnapshot(ctx(), "default", "vol1", "dup"))
	require.ErrorIs(t, f.CreateVolumeSnapshot(ctx(), "default", "vol1", "dup"), backend.ErrConflict)

	// Not-found paths: unknown pool, unknown volume, unknown snapshot.
	require.ErrorIs(t, f.CreateVolumeSnapshot(ctx(), "ghost", "vol1", "s"), backend.ErrNotFound)
	require.ErrorIs(t, f.CreateVolumeSnapshot(ctx(), "default", "ghost", "s"), backend.ErrNotFound)
	_, err = f.ListVolumeSnapshots(ctx(), "default", "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)
	require.ErrorIs(t, f.RestoreVolumeSnapshot(ctx(), "default", "vol1", "ghost"), backend.ErrNotFound)
	require.ErrorIs(t, f.DeleteVolumeSnapshot(ctx(), "default", "vol1", "ghost"), backend.ErrNotFound)
}

func TestVolumeSnapshotRenameAndExpiry(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateVolume(ctx(), "default", backend.StorageVolume{Name: "vol1", ContentType: "filesystem"}))
	require.NoError(t, f.CreateVolumeSnapshot(ctx(), "default", "vol1", "snap0"))

	// Set expiry; it surfaces on the listing.
	when := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, f.UpdateVolumeSnapshotExpiry(ctx(), "default", "vol1", "snap0", when))
	snaps, err := f.ListVolumeSnapshots(ctx(), "default", "vol1")
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.WithinDuration(t, when, snaps[0].ExpiresAt, 0)

	// Rename carries the expiry along.
	require.NoError(t, f.RenameVolumeSnapshot(ctx(), "default", "vol1", "snap0", "snap1"))
	snaps, err = f.ListVolumeSnapshots(ctx(), "default", "vol1")
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "snap1", snaps[0].Name)
	assert.WithinDuration(t, when, snaps[0].ExpiresAt, 0)

	// Clearing expiry with a zero time.
	require.NoError(t, f.UpdateVolumeSnapshotExpiry(ctx(), "default", "vol1", "snap1", time.Time{}))
	snaps, err = f.ListVolumeSnapshots(ctx(), "default", "vol1")
	require.NoError(t, err)
	assert.True(t, snaps[0].ExpiresAt.IsZero())

	// Rename onto an existing name conflicts; unknown snapshot is not-found.
	require.NoError(t, f.CreateVolumeSnapshot(ctx(), "default", "vol1", "other"))
	require.ErrorIs(t, f.RenameVolumeSnapshot(ctx(), "default", "vol1", "snap1", "other"), backend.ErrConflict)
	require.ErrorIs(t, f.RenameVolumeSnapshot(ctx(), "default", "vol1", "ghost", "x"), backend.ErrNotFound)
	require.ErrorIs(t, f.UpdateVolumeSnapshotExpiry(ctx(), "default", "vol1", "ghost", when), backend.ErrNotFound)

	// Incus parity: a malformed target name is rejected before the daemon op.
	require.ErrorIs(t, f.RenameVolumeSnapshot(ctx(), "default", "vol1", "snap1", "bad name"), backend.ErrInvalid)
}
