package fake

import (
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectCRUDRoundTrip(t *testing.T) {
	f := New()

	// The default project is seeded, like the daemon's.
	projects, err := f.ListProjects(ctx())
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "default", projects[0].Name)

	require.NoError(t, f.CreateProject(ctx(), "dev", "dev project", map[string]string{"features.profiles": "true"}))
	require.ErrorIs(t, f.CreateProject(ctx(), "dev", "", nil), backend.ErrConflict)
	require.ErrorIs(t, f.CreateProject(ctx(), "bad name", "", nil), backend.ErrInvalid)

	p, err := f.GetProject(ctx(), "dev")
	require.NoError(t, err)
	require.NotEmpty(t, p.Version)
	assert.Equal(t, "true", p.Config["features.profiles"])
	assert.Equal(t, "dev project", p.Description)

	// Update replaces description + config, conditionally on the version.
	require.NoError(t, f.UpdateProject(ctx(), "dev", "edited", map[string]string{"features.images": "false"}, p.Version))
	require.ErrorIs(t, f.UpdateProject(ctx(), "dev", "stale", nil, p.Version), backend.ErrConflict)
	got, err := f.GetProject(ctx(), "dev")
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, "false", got.Config["features.images"])
	assert.NotContains(t, got.Config, "features.profiles", "update replaces the whole config")

	// The default project can be neither renamed nor deleted, like the daemon.
	require.ErrorIs(t, f.RenameProject(ctx(), "default", "x"), backend.ErrInvalid)
	require.ErrorIs(t, f.DeleteProject(ctx(), "default"), backend.ErrInvalid)

	// Rename moves the project; collisions and bad names are rejected.
	require.NoError(t, f.RenameProject(ctx(), "dev", "dev2"))
	_, err = f.GetProject(ctx(), "dev")
	require.ErrorIs(t, err, backend.ErrNotFound)
	require.ErrorIs(t, f.RenameProject(ctx(), "dev2", "default"), backend.ErrConflict)
	require.ErrorIs(t, f.RenameProject(ctx(), "dev2", "bad name"), backend.ErrInvalid)

	require.NoError(t, f.DeleteProject(ctx(), "dev2"))
	require.ErrorIs(t, f.DeleteProject(ctx(), "dev2"), backend.ErrNotFound)
	require.ErrorIs(t, f.RenameProject(ctx(), "ghost", "x"), backend.ErrNotFound)
}
