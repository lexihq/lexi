package fake

import (
	"bytes"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/require"
)

func TestVolumeBackupLifecycle(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateStoragePool(ctx(), backend.StoragePool{Name: "pool", Driver: "dir"}))
	require.NoError(t, f.CreateVolume(ctx(), "pool", backend.StorageVolume{Name: "vol", Description: "data"}))

	// Empty name gets the daemon-style backupN default.
	require.NoError(t, f.CreateVolumeBackup(ctx(), "pool", "vol", "", time.Time{}, false))
	expiry := f.now().Add(24 * time.Hour)
	require.NoError(t, f.CreateVolumeBackup(ctx(), "pool", "vol", "weekly", expiry, true))

	err := f.CreateVolumeBackup(ctx(), "pool", "vol", "weekly", time.Time{}, false)
	require.ErrorContains(t, err, "already exists")
	err = f.CreateVolumeBackup(ctx(), "pool", "ghost", "", time.Time{}, false)
	require.Error(t, err) // volume not found

	bks, err := f.ListVolumeBackups(ctx(), "pool", "vol")
	require.NoError(t, err)
	require.Len(t, bks, 2)
	require.Equal(t, "backup0", bks[0].Name)
	require.Equal(t, "weekly", bks[1].Name)
	require.True(t, bks[1].VolumeOnly)
	require.Equal(t, expiry, bks[1].ExpiresAt)

	var buf bytes.Buffer
	require.NoError(t, f.ExportVolumeBackup(ctx(), "pool", "vol", "weekly", &buf))
	require.NotEmpty(t, buf.Bytes())

	// Restore-as into the same pool with a new name creates a new volume.
	require.NoError(t, f.RestoreVolumeBackup(ctx(), "pool", "vol", "weekly", "pool", "vol2"))
	vols, err := f.ListVolumes(ctx(), "pool")
	require.NoError(t, err)
	found := false
	for _, v := range vols {
		if v.Name == "vol2" {
			found = true
		}
	}
	require.True(t, found, "restored volume vol2 should exist")

	require.NoError(t, f.DeleteVolumeBackup(ctx(), "pool", "vol", "weekly"))
	err = f.DeleteVolumeBackup(ctx(), "pool", "vol", "weekly")
	require.Error(t, err) // gone
}
