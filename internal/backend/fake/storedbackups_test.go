package fake

import (
	"bytes"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstanceBackupLifecycle(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "bk", Image: "debian/12"}))

	// Empty name gets the daemon-style backupN default.
	require.NoError(t, f.CreateInstanceBackup(ctx(), "bk", "", time.Time{}, false))
	expiry := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, f.CreateInstanceBackup(ctx(), "bk", "weekly", expiry, true))

	// Duplicate names conflict; unknown instances are not found.
	err := f.CreateInstanceBackup(ctx(), "bk", "weekly", time.Time{}, false)
	require.ErrorIs(t, err, backend.ErrConflict)
	err = f.CreateInstanceBackup(ctx(), "ghost", "", time.Time{}, false)
	require.ErrorIs(t, err, backend.ErrNotFound)

	bks, err := f.ListInstanceBackups(ctx(), "bk")
	require.NoError(t, err)
	require.Len(t, bks, 2)
	assert.Equal(t, "backup0", bks[0].Name)
	assert.Equal(t, "weekly", bks[1].Name)
	assert.True(t, bks[1].InstanceOnly)
	assert.Equal(t, expiry, bks[1].ExpiresAt)

	// Download produces the export blob.
	var buf bytes.Buffer
	require.NoError(t, f.ExportInstanceBackup(ctx(), "bk", "weekly", &buf))
	assert.NotZero(t, buf.Len())

	// Restore-as creates a new instance from the stored backup.
	require.NoError(t, f.RestoreInstanceBackup(ctx(), "bk", "weekly", "bk2"))
	inst, err := f.GetInstance(ctx(), "bk2")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusStopped, inst.Status)
	// A taken name conflicts.
	err = f.RestoreInstanceBackup(ctx(), "bk", "weekly", "bk")
	require.ErrorIs(t, err, backend.ErrConflict)

	require.NoError(t, f.DeleteInstanceBackup(ctx(), "bk", "weekly"))
	err = f.DeleteInstanceBackup(ctx(), "bk", "weekly")
	require.ErrorIs(t, err, backend.ErrNotFound)
}
