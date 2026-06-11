//go:build integration

package incus

import (
	"context"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProjectCRUDRoundTrip creates a throwaway project, edits it (versioned),
// renames it, and deletes it; default-project guards and ghost lookups are
// asserted along the way.
func TestProjectCRUDRoundTrip(t *testing.T) {
	b := newBackend(t)
	if !b.Capabilities().Projects {
		t.Skip("daemon lacks the projects extension")
	}
	ctx := context.Background()
	name := uniqueName("lxproj")
	renamed := uniqueName("lxproj")
	t.Cleanup(func() { _ = b.DeleteProject(ctx, name); _ = b.DeleteProject(ctx, renamed) })

	require.NoError(t, b.CreateProject(ctx, name, "made by test", map[string]string{"features.profiles": "true"}))
	require.ErrorIs(t, b.CreateProject(ctx, name, "", nil), backend.ErrConflict)

	p, err := b.GetProject(ctx, name)
	require.NoError(t, err)
	require.NotEmpty(t, p.Version)
	assert.Equal(t, "made by test", p.Description)
	assert.Equal(t, "true", p.Config["features.profiles"])

	// Versioned update: stale etag conflicts after a successful write.
	cfg := p.Config
	cfg["user.lxcon"] = "yes"
	require.NoError(t, b.UpdateProject(ctx, name, "edited", cfg, p.Version))
	require.ErrorIs(t, b.UpdateProject(ctx, name, "stale", cfg, p.Version), backend.ErrConflict)
	got, err := b.GetProject(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, "yes", got.Config["user.lxcon"])

	// Default-project guards fire before any daemon call.
	require.ErrorIs(t, b.RenameProject(ctx, "default", uniqueName("x")), backend.ErrInvalid)
	require.ErrorIs(t, b.DeleteProject(ctx, "default"), backend.ErrInvalid)

	require.NoError(t, b.RenameProject(ctx, name, renamed))
	_, err = b.GetProject(ctx, name)
	require.ErrorIs(t, err, backend.ErrNotFound)

	require.NoError(t, b.DeleteProject(ctx, renamed))
	require.ErrorIs(t, b.DeleteProject(ctx, renamed), backend.ErrNotFound)
}
