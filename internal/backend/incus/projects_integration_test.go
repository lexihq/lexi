//go:build integration

package incus

import (
	"context"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProjectCRUDRoundTrip creates a throwaway project, edits it (versioned),
// renames it, and deletes it; default-project guards and ghost lookups are
// asserted along the way.
func TestProjectCRUDRoundTrip(t *testing.T) {
	b := newBackend(t)
	if !b.Capabilities(context.Background()).Projects {
		t.Skip("daemon lacks the projects extension")
	}
	ctx := context.Background()
	name := uniqueName("lxproj")
	renamed := uniqueName("lxproj")
	t.Cleanup(func() { _ = b.DeleteProject(ctx, name); _ = b.DeleteProject(ctx, renamed) })

	require.NoError(t, b.CreateProject(ctx, backend.Project{Name: name, Description: "made by test", Config: map[string]string{"features.profiles": "true"}}))
	require.ErrorIs(t, b.CreateProject(ctx, backend.Project{Name: name, Description: ""}), backend.ErrConflict)

	p, err := b.GetProject(ctx, name)
	require.NoError(t, err)
	require.NotEmpty(t, p.Version)
	assert.Equal(t, "made by test", p.Description)
	assert.Equal(t, "true", p.Config["features.profiles"])

	// Versioned update: stale etag conflicts after a successful write.
	cfg := p.Config
	cfg["user.lexi"] = "yes"
	require.NoError(t, b.UpdateProject(ctx, name, "edited", cfg, p.Version))
	require.ErrorIs(t, b.UpdateProject(ctx, name, "stale", cfg, p.Version), backend.ErrConflict)
	got, err := b.GetProject(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, "yes", got.Config["user.lexi"])

	// Default-project guards fire before any daemon call.
	require.ErrorIs(t, b.RenameProject(ctx, "default", uniqueName("x")), backend.ErrInvalid)
	require.ErrorIs(t, b.DeleteProject(ctx, "default"), backend.ErrInvalid)

	require.NoError(t, b.RenameProject(ctx, name, renamed))
	_, err = b.GetProject(ctx, name)
	require.ErrorIs(t, err, backend.ErrNotFound)

	require.NoError(t, b.DeleteProject(ctx, renamed))
	require.ErrorIs(t, b.DeleteProject(ctx, renamed), backend.ErrNotFound)
}

// TestProjectScopedInstanceIsolation creates an instance inside a throwaway
// project (sharing default's profiles and images so the create works without
// configuring a root disk) and asserts the two projects don't see each
// other's instances.
func TestProjectScopedInstanceIsolation(t *testing.T) {
	b := newBackend(t)
	if !b.Capabilities(context.Background()).Projects {
		t.Skip("daemon lacks the projects extension")
	}
	ctx := context.Background()
	project := uniqueName("lxscope")
	require.NoError(t, b.CreateProject(ctx, backend.Project{Name: project, Description: "made by test", Config: map[string]string{
		"features.profiles": "false", "features.images": "false",
	}}))
	scoped := backend.WithProject(ctx, project)
	name := uniqueName("scoped")
	t.Cleanup(func() {
		cleanupInstance(t, b, name) // default-project ghost cleanup is a no-op
		_ = b.DeleteInstance(scoped, name)
		_ = b.DeleteProject(ctx, project)
	})

	require.NoError(t, b.CreateInstance(scoped, backend.CreateOptions{Name: name, Image: testImage}))

	scopedList, err := b.ListInstances(scoped)
	require.NoError(t, err)
	require.True(t, listed(scopedList, name), "instance missing from its own project")

	defaultList, err := b.ListInstances(ctx)
	require.NoError(t, err)
	require.False(t, listed(defaultList, name), "instance leaked into the default project")

	// Metrics and state flow through the scoped client too.
	_, err = b.GetInstance(scoped, name)
	require.NoError(t, err)
	_, err = b.GetInstance(ctx, name)
	require.ErrorIs(t, err, backend.ErrNotFound)

	require.NoError(t, b.DeleteInstance(scoped, name))
	require.NoError(t, b.DeleteProject(ctx, project))
}

// TestGetProjectUsageRoundTrip creates a capped project and checks the state
// API reports the configured limits and zero usage for it.
func TestGetProjectUsageRoundTrip(t *testing.T) {
	b := newBackend(t)
	if !b.Capabilities(context.Background()).ProjectUsage {
		t.Skip("daemon lacks the project_usage extension")
	}
	ctx := context.Background()
	name := uniqueName("lxusage")
	t.Cleanup(func() { _ = b.DeleteProject(ctx, name) })
	require.NoError(t, b.CreateProject(ctx, backend.Project{Name: name, Description: "usage test", Config: map[string]string{
		"limits.instances": "5",
		"limits.memory":    "1GiB",
	}}))

	usage, err := b.GetProjectUsage(ctx, name)
	require.NoError(t, err)
	byName := map[string]backend.ProjectUsage{}
	for _, u := range usage {
		byName[u.Resource] = u
	}
	require.Contains(t, byName, "instances")
	assert.Equal(t, int64(0), byName["instances"].Usage)
	assert.Equal(t, int64(5), byName["instances"].Limit)
	require.Contains(t, byName, "memory")
	assert.Equal(t, int64(1<<30), byName["memory"].Limit)

	_, err = b.GetProjectUsage(ctx, uniqueName("ghost"))
	require.ErrorIs(t, err, backend.ErrNotFound)
}
