//go:build integration

package incus

import (
	"context"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
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

func TestProfileCRUDRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("lxprof")
	t.Cleanup(func() { _ = b.DeleteProfile(ctx, name) })

	require.NoError(t, b.CreateProfile(ctx, name, "made by test"))

	// Seed a device via the raw map (UpdateProfile must preserve it).
	p, err := b.GetProfile(ctx, name)
	require.NoError(t, err)
	require.NotEmpty(t, p.Version)

	require.NoError(t, b.UpdateProfile(ctx, name, "edited", map[string]string{"limits.cpu": "1"}, p.Version))

	got, err := b.GetProfile(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, "1", got.Config["limits.cpu"])

	// Stale etag must conflict.
	require.ErrorIs(t, b.UpdateProfile(ctx, name, "stale", nil, p.Version), backend.ErrConflict)

	require.NoError(t, b.DeleteProfile(ctx, name))
	_, err = b.GetProfile(ctx, name)
	require.ErrorIs(t, err, backend.ErrNotFound)

	require.ErrorIs(t, b.DeleteProfile(ctx, "default"), backend.ErrInvalid)
}

// TestProfileDeviceAndRenameRoundTrip exercises device add/update/remove (each
// GET-preserve-PUT, so config and other devices survive) and a rename.
func TestProfileDeviceAndRenameRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("lxprof")
	renamed := uniqueName("lxprof")
	t.Cleanup(func() { _ = b.DeleteProfile(ctx, name); _ = b.DeleteProfile(ctx, renamed) })

	require.NoError(t, b.CreateProfile(ctx, name, "made by test"))
	require.NoError(t, b.UpdateProfile(ctx, name, "keep", map[string]string{"limits.cpu": "1"}, ""))

	require.NoError(t, b.AddProfileDevice(ctx, name, "eth0", map[string]string{"type": "nic", "network": "incusbr0"}))
	p, err := b.GetProfile(ctx, name)
	require.NoError(t, err)
	require.Equal(t, "incusbr0", p.Devices["eth0"]["network"])
	require.Equal(t, "1", p.Config["limits.cpu"], "config survives device add")

	// Update conditional on the profile etag; a missing device is not-found.
	require.NoError(t, b.UpdateProfileDevice(ctx, name, "eth0", map[string]string{"type": "nic", "network": "incusbr0", "name": "eth0"}, p.Version))
	require.ErrorIs(t, b.UpdateProfileDevice(ctx, name, "ghost", nil, ""), backend.ErrNotFound)

	require.NoError(t, b.RemoveProfileDevice(ctx, name, "eth0"))
	p, err = b.GetProfile(ctx, name)
	require.NoError(t, err)
	require.NotContains(t, p.Devices, "eth0")

	// Rename carries config across; default cannot be renamed.
	require.NoError(t, b.RenameProfile(ctx, name, renamed))
	_, err = b.GetProfile(ctx, name)
	require.ErrorIs(t, err, backend.ErrNotFound)
	got, err := b.GetProfile(ctx, renamed)
	require.NoError(t, err)
	require.Equal(t, "1", got.Config["limits.cpu"])
	require.ErrorIs(t, b.RenameProfile(ctx, "default", uniqueName("x")), backend.ErrInvalid)
}

// The default profile always carries devices (root disk, eth0 nic), so a no-op
// description/config update against it proves UpdateProfile's GET-preserve-PUT
// does not drop the devices map.
func TestUpdateProfilePreservesDevicesIntegration(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()

	def, err := b.GetProfile(ctx, "default")
	require.NoError(t, err)
	require.NotEmpty(t, def.Devices)

	require.NoError(t, b.UpdateProfile(ctx, "default", def.Description, def.Config, def.Version))

	after, err := b.GetProfile(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, def.Devices, after.Devices, "no-op update must not drop devices")
}
