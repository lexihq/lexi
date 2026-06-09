//go:build integration

package incus

import (
	"context"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/require"
)

func TestRenameInstanceRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("mv")
	current := name
	t.Cleanup(func() { cleanupInstance(t, b, current) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	newName := name + "x"
	require.NoError(t, b.RenameInstance(ctx, name, newName))
	current = newName // so cleanup deletes the right instance

	_, err := b.GetInstance(ctx, newName)
	require.NoError(t, err)
	_, err = b.GetInstance(ctx, name)
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestMoveInstancePool(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()

	// Create a throwaway dir-backed pool to move into. Pool creation is out of the
	// app's scope, so drive the client directly here. Creating our own target (vs
	// skipping when the host has only one pool) is deliberate: a silent skip is
	// exactly what let the missing Migration flag ship.
	const pool = "lxmovepool"
	if err := b.srv.CreateStoragePool(api.StoragePoolsPost{Name: pool, Driver: "dir"}); err != nil {
		t.Skipf("cannot create a temp storage pool on this host: %v", err)
	}
	t.Cleanup(func() { _ = b.srv.DeleteStoragePool(pool) })

	name := uniqueName("mvp")
	t.Cleanup(func() { cleanupInstance(t, b, name) }) // LIFO: deletes instance before the pool
	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	// Stopped instance; a local cross-pool move copies the root disk.
	require.NoError(t, b.MoveInstance(ctx, name, pool))
}
