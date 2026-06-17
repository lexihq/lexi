//go:build integration

package incus

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVolumeStoredBackupLifecycle drives the full server-side volume backup
// flow against the real daemon: create (named + default-named), list, download,
// restore-as a new volume, delete.
func TestVolumeStoredBackupLifecycle(t *testing.T) {
	b := newBackend(t)
	if !b.Capabilities(context.Background()).VolumeStoredBackups {
		t.Skip("daemon lacks the custom_volume_backup extension")
	}
	ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelCtx()
	pool := pickPool(t, b, ctx)
	name := fmt.Sprintf("lxvsb%d", time.Now().UnixNano()%100000)
	restored := name + "r"
	t.Cleanup(func() {
		_ = b.DeleteVolume(ctx, pool.Name, name)
		_ = b.DeleteVolume(ctx, pool.Name, restored)
	})

	require.NoError(t, b.CreateVolume(ctx, pool.Name, backend.StorageVolume{
		Name: name, ContentType: "filesystem", Config: map[string]string{"size": "32MiB"},
	}))

	// Default-named backup plus a named one with expiry (when supported).
	require.NoError(t, b.CreateVolumeBackup(ctx, pool.Name, name, "", time.Time{}, false))
	expiry := time.Time{}
	if b.server(ctx).HasExtension("backup_expiry") {
		expiry = time.Now().Add(24 * time.Hour).UTC()
	}
	require.NoError(t, b.CreateVolumeBackup(ctx, pool.Name, name, "vb-keep", expiry, true))

	bks, err := b.ListVolumeBackups(ctx, pool.Name, name)
	require.NoError(t, err)
	require.Len(t, bks, 2)
	byName := map[string]backend.VolumeBackup{}
	for _, bk := range bks {
		byName[bk.Name] = bk
	}
	require.Contains(t, byName, "backup0")
	require.Contains(t, byName, "vb-keep")
	require.True(t, byName["vb-keep"].VolumeOnly)

	// Download produces a non-empty gzip tarball.
	var buf bytes.Buffer
	require.NoError(t, b.ExportVolumeBackup(ctx, pool.Name, name, "vb-keep", &buf))
	require.Greater(t, buf.Len(), 2)
	assert.Equal(t, []byte{0x1f, 0x8b}, buf.Bytes()[:2], "download should be a gzip stream")

	// Restore-as creates a new volume in the same pool from the stored backup.
	require.NoError(t, b.RestoreVolumeBackup(ctx, pool.Name, name, "vb-keep", pool.Name, restored))
	got, err := b.GetVolume(ctx, pool.Name, restored)
	require.NoError(t, err)
	assert.Equal(t, "32MiB", got.Config["size"], "config survives the round-trip")

	require.NoError(t, b.DeleteVolumeBackup(ctx, pool.Name, name, "vb-keep"))
	require.NoError(t, b.DeleteVolumeBackup(ctx, pool.Name, name, "backup0"))
	err = b.DeleteVolumeBackup(ctx, pool.Name, name, "vb-keep")
	require.ErrorIs(t, err, backend.ErrNotFound)
}
