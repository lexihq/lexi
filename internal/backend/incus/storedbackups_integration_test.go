//go:build integration

package incus

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/require"
)

// TestStoredBackupLifecycle drives the full server-side backup flow against
// the real daemon: create (named + default-named), list, download, restore
// under a new name, delete.
func TestStoredBackupLifecycle(t *testing.T) {
	b := newBackend(t)
	if !b.Capabilities(context.Background()).StoredBackups {
		t.Skip("daemon lacks container_backup")
	}
	ctx := context.Background()
	name := uniqueName("bkup")
	restored := uniqueName("bkup")
	t.Cleanup(func() { cleanupInstance(t, b, name); cleanupInstance(t, b, restored) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	// Default-named backup plus a named one with expiry (when supported).
	require.NoError(t, b.CreateInstanceBackup(ctx, name, "", time.Time{}, false))
	expiry := time.Time{}
	if b.server(ctx).HasExtension("backup_expiry") {
		expiry = time.Now().Add(24 * time.Hour).UTC()
	}
	require.NoError(t, b.CreateInstanceBackup(ctx, name, "it-keep", expiry, true))

	bks, err := b.ListInstanceBackups(ctx, name)
	require.NoError(t, err)
	require.Len(t, bks, 2)
	names := map[string]backend.InstanceBackup{}
	for _, bk := range bks {
		names[bk.Name] = bk
	}
	require.Contains(t, names, "backup0")
	require.Contains(t, names, "it-keep")
	require.True(t, names["it-keep"].InstanceOnly)

	// Download produces a non-empty tarball.
	var buf bytes.Buffer
	require.NoError(t, b.ExportInstanceBackup(ctx, name, "it-keep", &buf))
	require.NotZero(t, buf.Len())

	// Restore-as creates a new stopped instance entirely server-side.
	require.NoError(t, b.RestoreInstanceBackup(ctx, name, "it-keep", restored))
	inst, err := b.GetInstance(ctx, restored)
	require.NoError(t, err)
	require.Equal(t, "Stopped", inst.Status)

	require.NoError(t, b.DeleteInstanceBackup(ctx, name, "it-keep"))
	require.NoError(t, b.DeleteInstanceBackup(ctx, name, "backup0"))
	err = b.DeleteInstanceBackup(ctx, name, "it-keep")
	require.ErrorIs(t, err, backend.ErrNotFound)
}
