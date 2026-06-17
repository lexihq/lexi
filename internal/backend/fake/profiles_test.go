package fake

import (
	"testing"

	"github.com/lexihq/lexi/internal/backend"
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

func TestCreateProfile(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateProfile(ctx(), "web", "web servers"))

	p, err := f.GetProfile(ctx(), "web")
	require.NoError(t, err)
	assert.Equal(t, "web servers", p.Description)
	assert.NotEmpty(t, p.Version)

	require.ErrorIs(t, f.CreateProfile(ctx(), "web", ""), backend.ErrConflict)
}

func TestUpdateProfilePreservesDevices(t *testing.T) {
	f := New()
	// The seeded default profile carries eth0+root devices.
	p, err := f.GetProfile(ctx(), "default")
	require.NoError(t, err)
	require.NotEmpty(t, p.Devices)

	require.NoError(t, f.UpdateProfile(ctx(), "default", "edited", map[string]string{"limits.cpu": "2"}, p.Version))

	got, err := f.GetProfile(ctx(), "default")
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, map[string]string{"limits.cpu": "2"}, got.Config)
	assert.Equal(t, p.Devices, got.Devices, "devices must survive a config update")
	assert.NotEqual(t, p.Version, got.Version)
}

func TestUpdateProfileStaleVersionConflicts(t *testing.T) {
	f := New()
	p, err := f.GetProfile(ctx(), "default")
	require.NoError(t, err)
	require.NoError(t, f.UpdateProfile(ctx(), "default", "first", nil, p.Version))
	require.ErrorIs(t, f.UpdateProfile(ctx(), "default", "second", nil, p.Version), backend.ErrConflict)
	// Empty version updates unconditionally.
	require.NoError(t, f.UpdateProfile(ctx(), "default", "second", nil, ""))
}

func TestUpdateProfileNotFound(t *testing.T) {
	f := New()
	require.ErrorIs(t, f.UpdateProfile(ctx(), "ghost", "", nil, ""), backend.ErrNotFound)
}

func TestProfileDeviceAddUpdateRemove(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateProfile(ctx(), "web", ""))

	// Add bumps the version and surfaces on GetProfile.
	require.NoError(t, f.AddProfileDevice(ctx(), "web", "eth0", map[string]string{"type": "nic", "network": "lxdbr0"}))
	p, err := f.GetProfile(ctx(), "web")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"type": "nic", "network": "lxdbr0"}, p.Devices["eth0"])

	// Update replaces the device's config; stale version conflicts.
	require.NoError(t, f.UpdateProfileDevice(ctx(), "web", "eth0", map[string]string{"type": "nic", "network": "br1"}, p.Version))
	require.ErrorIs(t, f.UpdateProfileDevice(ctx(), "web", "eth0", map[string]string{"type": "nic"}, p.Version), backend.ErrConflict)
	p, err = f.GetProfile(ctx(), "web")
	require.NoError(t, err)
	assert.Equal(t, "br1", p.Devices["eth0"]["network"])

	// Update/remove of a missing device is not-found.
	require.ErrorIs(t, f.UpdateProfileDevice(ctx(), "web", "ghost", nil, ""), backend.ErrNotFound)
	require.ErrorIs(t, f.RemoveProfileDevice(ctx(), "web", "ghost"), backend.ErrNotFound)

	require.NoError(t, f.RemoveProfileDevice(ctx(), "web", "eth0"))
	p, err = f.GetProfile(ctx(), "web")
	require.NoError(t, err)
	assert.NotContains(t, p.Devices, "eth0")
}

func TestRenameProfile(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateProfile(ctx(), "web", "desc"))
	require.NoError(t, f.AddProfileDevice(ctx(), "web", "eth0", map[string]string{"type": "nic"}))

	require.NoError(t, f.RenameProfile(ctx(), "web", "frontend"))
	_, err := f.GetProfile(ctx(), "web")
	require.ErrorIs(t, err, backend.ErrNotFound)
	got, err := f.GetProfile(ctx(), "frontend")
	require.NoError(t, err)
	assert.Equal(t, "desc", got.Description)
	assert.Contains(t, got.Devices, "eth0", "devices carry across rename")

	// default cannot be renamed; target name must be free and well-formed
	// (reserved characters rejected like the daemon's IsAPIName).
	require.ErrorIs(t, f.RenameProfile(ctx(), "default", "x"), backend.ErrInvalid)
	require.NoError(t, f.CreateProfile(ctx(), "other", ""))
	require.ErrorIs(t, f.RenameProfile(ctx(), "other", "frontend"), backend.ErrConflict)
	require.ErrorIs(t, f.RenameProfile(ctx(), "frontend", "bad name"), backend.ErrInvalid)
	require.ErrorIs(t, f.RenameProfile(ctx(), "frontend", "bad$name"), backend.ErrInvalid)
}

func TestRenameProfileFollowsAssignedInstances(t *testing.T) {
	f := New()
	mustCreate(t, f, "demo")
	require.NoError(t, f.CreateProfile(ctx(), "web", ""))
	require.NoError(t, f.SetInstanceProfiles(ctx(), "demo", []string{"default", "web"}))

	require.NoError(t, f.RenameProfile(ctx(), "web", "frontend"))

	inst, err := f.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"default", "frontend"}, inst.Profiles, "assigned instances follow the rename")
	got, err := f.GetProfile(ctx(), "frontend")
	require.NoError(t, err)
	assert.Contains(t, got.UsedBy, "demo")
}

func TestDeleteProfile(t *testing.T) {
	f := New()
	require.NoError(t, f.DeleteProfile(ctx(), "gpu"))
	_, err := f.GetProfile(ctx(), "gpu")
	require.ErrorIs(t, err, backend.ErrNotFound)

	require.ErrorIs(t, f.DeleteProfile(ctx(), "ghost"), backend.ErrNotFound)
	require.ErrorIs(t, f.DeleteProfile(ctx(), "default"), backend.ErrInvalid)
}

func TestDeleteProfileInUseConflicts(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateProfile(ctx(), "web", ""))
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo"}))
	require.NoError(t, f.SetInstanceProfiles(ctx(), "demo", []string{"default", "web"}))
	require.ErrorIs(t, f.DeleteProfile(ctx(), "web"), backend.ErrConflict)
}

func TestCreateProfileInvalidNameIsInvalid(t *testing.T) {
	f := New()
	// Incus parity: API object names exclude whitespace and path separators.
	require.ErrorIs(t, f.CreateProfile(ctx(), "has space", ""), backend.ErrInvalid)
	require.ErrorIs(t, f.CreateProfile(ctx(), "a/b", ""), backend.ErrInvalid)
}
