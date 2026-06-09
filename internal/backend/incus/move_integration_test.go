//go:build integration

package incus

import (
	"context"
	"testing"

	"github.com/adam/lxcon/internal/backend"
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
	pools, err := b.ListStoragePools(ctx)
	require.NoError(t, err)
	// Pick a target pool other than the default (instances land on "default").
	target := ""
	for _, p := range pools {
		if p.Name != "default" {
			target = p.Name
			break
		}
	}
	if target == "" {
		t.Skip("no non-default storage pool to move to")
	}

	name := uniqueName("mvp")
	t.Cleanup(func() { cleanupInstance(t, b, name) })
	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	// Stopped instance; a local cross-pool move copies the root disk.
	require.NoError(t, b.MoveInstance(ctx, name, target))
}
