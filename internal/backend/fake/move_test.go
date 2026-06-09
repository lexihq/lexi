package fake

import (
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenameInstance(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	require.NoError(t, b.RenameInstance(ctx(), "demo", "renamed"))
	_, err := b.GetInstance(ctx(), "demo")
	require.ErrorIs(t, err, backend.ErrNotFound)
	got, err := b.GetInstance(ctx(), "renamed")
	require.NoError(t, err)
	assert.Equal(t, "renamed", got.Name)

	// Missing source and name collision.
	require.ErrorIs(t, b.RenameInstance(ctx(), "ghost", "x"), backend.ErrNotFound)
	mustCreate(t, b, "other")
	require.ErrorIs(t, b.RenameInstance(ctx(), "other", "renamed"), backend.ErrConflict)
}

func TestMoveInstance(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	require.NoError(t, b.MoveInstance(ctx(), "demo", "zfs0")) // seeded pool
	require.ErrorIs(t, b.MoveInstance(ctx(), "demo", "ghostpool"), backend.ErrNotFound)
	require.ErrorIs(t, b.MoveInstance(ctx(), "ghost", "zfs0"), backend.ErrNotFound)
}
