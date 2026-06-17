//go:build integration

package incus

import (
	"context"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateLimitsRoundTrip sets and clears limits on a throwaway container and
// reads them back through the expanded config.
func TestUpdateLimitsRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("limits")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))
	require.NoError(t, b.UpdateLimits(ctx, name, backend.Limits{CPU: "2", Memory: "256MiB"}))

	inst, err := b.GetInstance(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "2", inst.LimitsCPU)
	assert.Equal(t, "256MiB", inst.LimitsMemory)

	// Empty limits clear the keys.
	require.NoError(t, b.UpdateLimits(ctx, name, backend.Limits{}))
	inst, err = b.GetInstance(ctx, name)
	require.NoError(t, err)
	assert.Empty(t, inst.LimitsCPU)
	assert.Empty(t, inst.LimitsMemory)
}

func TestInstanceConfigRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("cfg")
	t.Cleanup(func() { cleanupInstance(t, b, name) })
	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	require.NoError(t, b.UpdateInstanceConfig(ctx, name, map[string]string{"security.nesting": "true"}))
	cfg, err := b.GetInstanceConfig(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "true", cfg.Config["security.nesting"])
	assert.NotEmpty(t, cfg.Devices) // expanded devices from the default profile

	require.NoError(t, b.UpdateInstanceConfig(ctx, name, map[string]string{}))
	cfg, err = b.GetInstanceConfig(ctx, name)
	require.NoError(t, err)
	_, present := cfg.Config["security.nesting"]
	assert.False(t, present)
}

func TestDeviceAddRemoveRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("dev")
	t.Cleanup(func() { cleanupInstance(t, b, name) })
	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	require.NoError(t, b.AddDevice(ctx, name, "web",
		map[string]string{"type": "proxy", "listen": "tcp:127.0.0.1:8080", "connect": "tcp:127.0.0.1:80"}))
	cfg, err := b.GetInstanceConfig(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "proxy", cfg.LocalDevices["web"]["type"])
	assert.NotContains(t, cfg.LocalDevices, "root") // root stays inherited

	require.NoError(t, b.RemoveDevice(ctx, name, "web"))
	cfg, err = b.GetInstanceConfig(ctx, name)
	require.NoError(t, err)
	assert.NotContains(t, cfg.LocalDevices, "web")

	require.ErrorIs(t, b.RemoveDevice(ctx, name, "web"), backend.ErrNotFound)
}

func TestUpdateDeviceRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("devedit")
	t.Cleanup(func() { cleanupInstance(t, b, name) })
	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	require.NoError(t, b.AddDevice(ctx, name, "web",
		map[string]string{"type": "proxy", "listen": "tcp:127.0.0.1:8080", "connect": "tcp:127.0.0.1:80"}))

	cfg, err := b.GetInstanceConfig(ctx, name)
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Version)

	require.NoError(t, b.UpdateDevice(ctx, name, "web",
		map[string]string{"type": "proxy", "listen": "tcp:127.0.0.1:9090", "connect": "tcp:127.0.0.1:80"}, cfg.Version))

	got, err := b.GetInstanceConfig(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "tcp:127.0.0.1:9090", got.LocalDevices["web"]["listen"])

	// Replaying the pre-update version must conflict (412 → ErrConflict).
	err = b.UpdateDevice(ctx, name, "web",
		map[string]string{"type": "proxy", "listen": "tcp:127.0.0.1:7070", "connect": "tcp:127.0.0.1:80"}, cfg.Version)
	require.ErrorIs(t, err, backend.ErrConflict)

	require.ErrorIs(t, b.UpdateDevice(ctx, name, "ghost", map[string]string{"type": "none"}, ""), backend.ErrNotFound)
}
