//go:build integration

package incus

import (
	"context"
	"testing"

	"github.com/adam/lxcon/internal/backend"
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
