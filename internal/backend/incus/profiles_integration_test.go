//go:build integration

package incus

import (
	"context"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfilesListAndAssign(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()

	profiles, err := b.ListProfiles(ctx)
	require.NoError(t, err)
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, p.Name)
	}
	require.Contains(t, names, "default")

	_, err = b.GetProfile(ctx, "default")
	require.NoError(t, err)

	name := uniqueName("prof")
	t.Cleanup(func() { cleanupInstance(t, b, name) })
	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))
	require.NoError(t, b.SetInstanceProfiles(ctx, name, []string{"default"}))
	inst, err := b.GetInstance(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, inst.Profiles)
}
