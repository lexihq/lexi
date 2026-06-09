package fake

import (
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfilesListAndGet(t *testing.T) {
	f := New()
	profiles, err := f.ListProfiles(ctx())
	require.NoError(t, err)
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, p.Name)
	}
	assert.Contains(t, names, "default")
	assert.Contains(t, names, "gpu")

	gpu, err := f.GetProfile(ctx(), "gpu")
	require.NoError(t, err)
	assert.NotEmpty(t, gpu.Devices, "gpu profile should carry a sample device")

	_, err = f.GetProfile(ctx(), "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestNewInstanceDefaultsToDefaultProfile(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo"}))
	inst, err := f.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, inst.Profiles)
}

func TestSetInstanceProfiles(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo"}))

	require.NoError(t, f.SetInstanceProfiles(ctx(), "demo", []string{"default", "gpu"}))
	inst, err := f.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"default", "gpu"}, inst.Profiles)

	gpu, err := f.GetProfile(ctx(), "gpu")
	require.NoError(t, err)
	assert.Contains(t, gpu.UsedBy, "demo")

	require.ErrorIs(t, f.SetInstanceProfiles(ctx(), "demo", []string{"nope"}), backend.ErrInvalid)
	require.ErrorIs(t, f.SetInstanceProfiles(ctx(), "ghost", []string{"default"}), backend.ErrNotFound)
}
