//go:build integration

package incus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkCRUDRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	// Bridge names map to a Linux interface (≤15 chars), so keep it short.
	name := fmt.Sprintf("lxnet%d", time.Now().UnixNano()%100000)
	t.Cleanup(func() { _ = b.DeleteNetwork(ctx, name) })

	// Explicit subnet so the test doesn't depend on Incus auto-allocating one
	// (which fails on hosts with many bridges already).
	require.NoError(t, b.CreateNetwork(ctx, backend.Network{
		Name: name, Type: "bridge",
		Config: map[string]string{"ipv4.address": "10.99.0.1/24", "ipv4.nat": "true", "ipv6.address": "none"},
	}))
	n, err := b.GetNetwork(ctx, name)
	require.NoError(t, err)
	assert.True(t, n.Managed)
	assert.Equal(t, "bridge", n.Type)

	require.NoError(t, b.DeleteNetwork(ctx, name))
	_, err = b.GetNetwork(ctx, name)
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestNetworkUpdateRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := fmt.Sprintf("lxnet%d", time.Now().UnixNano()%100000)
	t.Cleanup(func() { _ = b.DeleteNetwork(ctx, name) })

	require.NoError(t, b.CreateNetwork(ctx, backend.Network{
		Name: name, Type: "bridge",
		Config: map[string]string{"ipv4.address": "10.98.0.1/24", "ipv4.nat": "true", "ipv6.address": "none"},
	}))

	n, err := b.GetNetwork(ctx, name)
	require.NoError(t, err)
	require.NotEmpty(t, n.Version)

	cfg := map[string]string{"ipv4.address": "10.98.0.1/24", "ipv4.nat": "false", "ipv6.address": "none"}
	require.NoError(t, b.UpdateNetwork(ctx, name, "updated by test", cfg, n.Version))

	got, err := b.GetNetwork(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "updated by test", got.Description)
	assert.Equal(t, "false", got.Config["ipv4.nat"])

	// Replaying the pre-update version must conflict (412 → ErrConflict).
	err = b.UpdateNetwork(ctx, name, "stale write", cfg, n.Version)
	require.ErrorIs(t, err, backend.ErrConflict)
}
