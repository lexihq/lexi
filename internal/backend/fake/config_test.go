package fake

import (
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateLimits(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	require.NoError(t, b.UpdateLimits(ctx(), "demo", backend.Limits{CPU: "2", Memory: "2GiB"}))
	inst, err := b.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "2", inst.LimitsCPU)
	assert.Equal(t, "2GiB", inst.LimitsMemory)

	// Empty limits clear the values.
	require.NoError(t, b.UpdateLimits(ctx(), "demo", backend.Limits{}))
	inst, err = b.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Empty(t, inst.LimitsCPU)
	assert.Empty(t, inst.LimitsMemory)

	// Missing instance → ErrNotFound.
	require.ErrorIs(t, b.UpdateLimits(ctx(), "ghost", backend.Limits{}), backend.ErrNotFound)
}

func TestInstanceConfigRoundTrip(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo"}))

	cfg, err := f.GetInstanceConfig(ctx(), "demo")
	require.NoError(t, err)
	assert.Empty(t, cfg.Config)
	assert.Contains(t, cfg.Devices, "root") // from the "default" profile

	require.NoError(t, f.UpdateInstanceConfig(ctx(), "demo", map[string]string{"security.nesting": "true"}))
	cfg, err = f.GetInstanceConfig(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "true", cfg.Config["security.nesting"])

	require.NoError(t, f.UpdateInstanceConfig(ctx(), "demo", map[string]string{}))
	cfg, err = f.GetInstanceConfig(ctx(), "demo")
	require.NoError(t, err)
	assert.Empty(t, cfg.Config)

	_, err = f.GetInstanceConfig(ctx(), "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)
	require.ErrorIs(t, f.UpdateInstanceConfig(ctx(), "ghost", nil), backend.ErrNotFound)
}

func TestGetInstanceConfigSeparatesLocalFromInherited(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo"}))

	cfg, err := f.GetInstanceConfig(ctx(), "demo")
	require.NoError(t, err)
	assert.Empty(t, cfg.LocalDevices)       // nothing local yet
	assert.Contains(t, cfg.Devices, "root") // inherited from "default"
}

func TestDeviceAddRemoveRoundTrip(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo"}))

	require.NoError(t, f.AddDevice(ctx(), "demo", "web",
		map[string]string{"type": "proxy", "listen": "tcp:0.0.0.0:80", "connect": "tcp:127.0.0.1:80"}))

	cfg, err := f.GetInstanceConfig(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "proxy", cfg.LocalDevices["web"]["type"])
	assert.Contains(t, cfg.Devices, "web") // shows in expanded too

	// Local overrides a same-named profile device in the expanded view.
	require.NoError(t, f.AddDevice(ctx(), "demo", "root", map[string]string{"type": "disk", "path": "/srv"}))
	cfg, err = f.GetInstanceConfig(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "/srv", cfg.Devices["root"]["path"])

	require.NoError(t, f.RemoveDevice(ctx(), "demo", "web"))
	cfg, err = f.GetInstanceConfig(ctx(), "demo")
	require.NoError(t, err)
	assert.NotContains(t, cfg.LocalDevices, "web")

	// Error paths.
	require.ErrorIs(t, f.AddDevice(ctx(), "ghost", "x", map[string]string{"type": "disk"}), backend.ErrNotFound)
	require.ErrorIs(t, f.RemoveDevice(ctx(), "demo", "nope"), backend.ErrNotFound)
}

func TestAddDeviceToClonedInstance(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "src"}))
	require.NoError(t, f.CloneInstance(ctx(), "src", "dst"))

	// Cloned instances have no seeded device map; AddDevice must not panic.
	require.NoError(t, f.AddDevice(ctx(), "dst", "web", map[string]string{"type": "proxy"}))
	cfg, err := f.GetInstanceConfig(ctx(), "dst")
	require.NoError(t, err)
	assert.Equal(t, "proxy", cfg.LocalDevices["web"]["type"])
}

func TestUpdateDeviceReplacesConfig(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo"}))
	require.NoError(t, f.AddDevice(ctx(), "demo", "web", map[string]string{"type": "proxy", "listen": "tcp:0.0.0.0:80", "user.note": "keep"}))

	cfg, err := f.GetInstanceConfig(ctx(), "demo")
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Version)

	require.NoError(t, f.UpdateDevice(ctx(), "demo", "web",
		map[string]string{"type": "proxy", "listen": "tcp:0.0.0.0:8080", "user.note": "keep"}, cfg.Version))

	got, err := f.GetInstanceConfig(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "tcp:0.0.0.0:8080", got.LocalDevices["web"]["listen"])
	assert.Equal(t, "keep", got.LocalDevices["web"]["user.note"])
	assert.NotEqual(t, cfg.Version, got.Version, "version must change on device update")
}

func TestUpdateDeviceStaleVersionConflicts(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo"}))
	require.NoError(t, f.AddDevice(ctx(), "demo", "web", map[string]string{"type": "proxy"}))

	cfg, err := f.GetInstanceConfig(ctx(), "demo")
	require.NoError(t, err)
	require.NoError(t, f.UpdateDevice(ctx(), "demo", "web", map[string]string{"type": "proxy", "a": "1"}, cfg.Version))
	require.ErrorIs(t, f.UpdateDevice(ctx(), "demo", "web", map[string]string{"type": "proxy", "a": "2"}, cfg.Version), backend.ErrConflict)
	// Empty version updates unconditionally.
	require.NoError(t, f.UpdateDevice(ctx(), "demo", "web", map[string]string{"type": "proxy", "a": "2"}, ""))
}

func TestUpdateDeviceMissingIsNotFound(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo"}))
	require.ErrorIs(t, f.UpdateDevice(ctx(), "demo", "ghost", map[string]string{"type": "disk"}, ""), backend.ErrNotFound)
	require.ErrorIs(t, f.UpdateDevice(ctx(), "ghost", "web", map[string]string{"type": "disk"}, ""), backend.ErrNotFound)
}

func TestLifecycleChangeInvalidatesConfigVersion(t *testing.T) {
	// Incus parity: the instance etag covers the whole object, so a lifecycle
	// change between form render and save must conflict the device edit.
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo"}))
	require.NoError(t, f.AddDevice(ctx(), "demo", "web", map[string]string{"type": "proxy"}))

	cfg, err := f.GetInstanceConfig(ctx(), "demo")
	require.NoError(t, err)
	require.NoError(t, f.StartInstance(ctx(), "demo"))

	require.ErrorIs(t, f.UpdateDevice(ctx(), "demo", "web", map[string]string{"type": "proxy", "a": "1"}, cfg.Version), backend.ErrConflict)
}
