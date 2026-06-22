//go:build integration

package incus

import (
	"context"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/require"
)

// TestMigrateInstanceSameRemoteRename exercises the real migration machinery
// (server-to-server CopyInstance + source delete) on a single host by
// targeting the same remote under a new name — the UI forbids same-remote
// targets, but the driver deliberately doesn't, for exactly this test.
func TestMigrateInstanceSameRemoteRename(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	src := uniqueName("mig")
	dst := uniqueName("mig")
	t.Cleanup(func() { cleanupInstance(t, b, src); cleanupInstance(t, b, dst) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: src, Image: testImage}))

	require.NoError(t, b.MigrateInstance(ctx, src, b.remoteName, dst))

	_, err := b.GetInstance(ctx, src)
	require.ErrorIs(t, err, backend.ErrNotFound, "source must be gone after migration")
	inst, err := b.GetInstance(ctx, dst)
	require.NoError(t, err)
	require.Equal(t, backend.StatusStopped, inst.Status)

	// A running instance is refused before any transfer starts.
	require.NoError(t, b.StartInstance(ctx, dst))
	err = b.MigrateInstance(ctx, dst, b.remoteName, uniqueName("mig"))
	require.ErrorIs(t, err, backend.ErrInvalid)
	require.NoError(t, b.StopInstance(ctx, dst))

	// Unknown target remote.
	err = b.MigrateInstance(ctx, dst, "ghost-remote", "")
	require.ErrorIs(t, err, backend.ErrNotFound)
}
