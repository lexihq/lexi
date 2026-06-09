package fake

import (
	"testing"

	"github.com/adam/lxcon/internal/backend"
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
