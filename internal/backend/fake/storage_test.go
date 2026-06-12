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

func TestUpdateStoragePoolReplacesConfigAndDescription(t *testing.T) {
	f := New()
	p, err := f.GetStoragePool(ctx(), "default")
	require.NoError(t, err)
	require.NotEmpty(t, p.Version)

	require.NoError(t, f.UpdateStoragePool(ctx(), "default", "edited", map[string]string{"rsync.bwlimit": "10MiB"}, p.Version))

	got, err := f.GetStoragePool(ctx(), "default")
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, map[string]string{"rsync.bwlimit": "10MiB"}, got.Config)
	assert.NotEqual(t, p.Version, got.Version, "version bumps on update")
}

func TestUpdateStoragePoolStaleVersionConflicts(t *testing.T) {
	f := New()
	p, err := f.GetStoragePool(ctx(), "default")
	require.NoError(t, err)
	require.NoError(t, f.UpdateStoragePool(ctx(), "default", "first", nil, p.Version))
	require.ErrorIs(t, f.UpdateStoragePool(ctx(), "default", "second", nil, p.Version), backend.ErrConflict)
	// Empty version updates unconditionally.
	require.NoError(t, f.UpdateStoragePool(ctx(), "default", "second", nil, ""))
}

func TestUpdateStoragePoolNotFound(t *testing.T) {
	require.ErrorIs(t, New().UpdateStoragePool(ctx(), "ghost", "", nil, ""), backend.ErrNotFound)
}

func TestUpdateVolumeReplacesConfigAndDescription(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateVolume(ctx(), "default", backend.StorageVolume{Name: "vol1", ContentType: "filesystem", Config: map[string]string{"size": "1GiB"}}))
	v, err := f.GetVolume(ctx(), "default", "vol1")
	require.NoError(t, err)
	require.NotEmpty(t, v.Version)

	require.NoError(t, f.UpdateVolume(ctx(), "default", "vol1", "edited", map[string]string{"size": "2GiB"}, v.Version))

	got, err := f.GetVolume(ctx(), "default", "vol1")
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, "2GiB", got.Config["size"], "resize via the size key")
	assert.NotEqual(t, v.Version, got.Version)

	// Stale version conflicts; empty version is unconditional; ghost is 404.
	require.ErrorIs(t, f.UpdateVolume(ctx(), "default", "vol1", "stale", nil, v.Version), backend.ErrConflict)
	require.NoError(t, f.UpdateVolume(ctx(), "default", "vol1", "uncond", nil, ""))
	require.ErrorIs(t, f.UpdateVolume(ctx(), "default", "ghost", "", nil, ""), backend.ErrNotFound)
}

func TestRenameVolume(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateVolume(ctx(), "default", backend.StorageVolume{Name: "vol1", ContentType: "filesystem"}))
	require.NoError(t, f.CreateVolumeSnapshot(ctx(), "default", "vol1", "snap0"))

	require.NoError(t, f.RenameVolume(ctx(), "default", "vol1", "vol2"))
	_, err := f.GetVolume(ctx(), "default", "vol1")
	require.ErrorIs(t, err, backend.ErrNotFound)
	got, err := f.GetVolume(ctx(), "default", "vol2")
	require.NoError(t, err)
	assert.Equal(t, "vol2", got.Name)
	snaps, err := f.ListVolumeSnapshots(ctx(), "default", "vol2")
	require.NoError(t, err)
	assert.Len(t, snaps, 1, "snapshots carry across rename")

	// Target name must be free and well-formed; ghost is 404.
	require.NoError(t, f.CreateVolume(ctx(), "default", backend.StorageVolume{Name: "other"}))
	require.ErrorIs(t, f.RenameVolume(ctx(), "default", "vol2", "other"), backend.ErrConflict)
	require.ErrorIs(t, f.RenameVolume(ctx(), "default", "vol2", "bad name"), backend.ErrInvalid)
	require.ErrorIs(t, f.RenameVolume(ctx(), "default", "vol2", "bad?name"), backend.ErrInvalid)
	require.ErrorIs(t, f.RenameVolume(ctx(), "default", "ghost", "x"), backend.ErrNotFound)
}

func TestRenameVolumeFollowsAndRefusesInstanceReferences(t *testing.T) {
	f := New()
	mustCreate(t, f, "demo")
	require.NoError(t, f.CreateVolume(ctx(), "default", backend.StorageVolume{Name: "data"}))
	require.NoError(t, f.AddDevice(ctx(), "demo", "data", map[string]string{
		"type": "disk", "pool": "default", "source": "data", "path": "/mnt",
	}))

	// Stopped instance: the disk-device reference follows the rename.
	require.NoError(t, f.RenameVolume(ctx(), "default", "data", "data2"))
	cfg, err := f.GetInstanceConfig(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "data2", cfg.LocalDevices["data"]["source"], "device reference follows the rename")

	// Running instance: the rename is refused, like the daemon.
	require.NoError(t, f.StartInstance(ctx(), "demo"))
	require.ErrorIs(t, f.RenameVolume(ctx(), "default", "data2", "data3"), backend.ErrInvalid)
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

func TestVolumeExportImportRoundTrip(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateVolume(ctx(), "default", backend.StorageVolume{
		Name: "vol-x", Description: "scratch data", Config: map[string]string{"size": "1GiB"},
	}))

	var buf strings.Builder
	require.NoError(t, f.ExportVolume(ctx(), "default", "vol-x", &buf))
	require.NotEmpty(t, buf.String())

	// Ghosts are not found.
	require.ErrorIs(t, f.ExportVolume(ctx(), "default", "ghost", &strings.Builder{}), backend.ErrNotFound)
	require.ErrorIs(t, f.ExportVolume(ctx(), "ghost", "vol-x", &strings.Builder{}), backend.ErrNotFound)

	// Import collides with the existing volume, succeeds after delete, and
	// recovers description + config so the round-trip is observable.
	require.ErrorIs(t, f.ImportVolume(ctx(), "default", "vol-x", strings.NewReader(buf.String())), backend.ErrConflict)
	require.NoError(t, f.DeleteVolume(ctx(), "default", "vol-x"))
	require.NoError(t, f.ImportVolume(ctx(), "default", "vol-x", strings.NewReader(buf.String())))

	got, err := f.GetVolume(ctx(), "default", "vol-x")
	require.NoError(t, err)
	assert.Equal(t, "scratch data", got.Description)
	assert.Equal(t, "1GiB", got.Config["size"])

	// Foreign blobs are invalid; ghost target pools are not found.
	require.ErrorIs(t, f.ImportVolume(ctx(), "default", "other", strings.NewReader("garbage")), backend.ErrInvalid)
	require.ErrorIs(t, f.ImportVolume(ctx(), "ghost", "other", strings.NewReader(buf.String())), backend.ErrNotFound)
}

func TestCreateVolumeFromISO(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateVolumeFromISO(ctx(), "default", "install-media", strings.NewReader("iso-bytes")))

	v, err := f.GetVolume(ctx(), "default", "install-media")
	require.NoError(t, err)
	assert.Equal(t, "custom", v.Type)
	assert.Equal(t, "iso", v.ContentType)
}

func TestCreateVolumeFromISOErrors(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateVolumeFromISO(ctx(), "default", "install-media", strings.NewReader("iso-bytes")))

	// The name is the volume's identity: a second upload conflicts.
	require.ErrorIs(t, f.CreateVolumeFromISO(ctx(), "default", "install-media", strings.NewReader("x")), backend.ErrConflict)
	// An ISO volume also collides with an existing custom volume.
	require.NoError(t, f.CreateVolume(ctx(), "default", backend.StorageVolume{Name: "vol1"}))
	require.ErrorIs(t, f.CreateVolumeFromISO(ctx(), "default", "vol1", strings.NewReader("x")), backend.ErrConflict)
	// Ghost pools are not found; invalid names are rejected.
	require.ErrorIs(t, f.CreateVolumeFromISO(ctx(), "ghost", "other", strings.NewReader("x")), backend.ErrNotFound)
	require.ErrorIs(t, f.CreateVolumeFromISO(ctx(), "default", "bad name", strings.NewReader("x")), backend.ErrInvalid)
}

func TestCapabilitiesReportISOVolumes(t *testing.T) {
	assert.True(t, New().Capabilities(ctx()).ISOVolumes)
}
