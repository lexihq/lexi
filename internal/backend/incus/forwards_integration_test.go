//go:build integration

package incus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/require"
)

// forwardTestNetwork creates a managed bridge for forward tests; forwards on
// the shared default bridge could collide with host services. The name stays
// under the kernel 15-character interface limit, which uniqueName exceeds.
func forwardTestNetwork(t *testing.T, b *incusBackend) string {
	t.Helper()
	name := fmt.Sprintf("itfw%d", time.Now().UnixNano()%1e9)
	require.NoError(t, b.CreateNetwork(context.Background(), backend.Network{
		Name: name, Type: "bridge",
		Config: map[string]string{"ipv4.address": "10.99.77.1/24", "ipv4.nat": "true", "ipv6.address": "none"},
	}))
	t.Cleanup(func() {
		if err := b.DeleteNetwork(context.Background(), name); err != nil {
			t.Logf("cleanup network %q: %v", name, err)
		}
	})
	return name
}

func TestNetworkForwardLifecycleIntegration(t *testing.T) {
	b := newBackend(t)
	if !b.Capabilities(context.Background()).NetworkForwards {
		t.Skip("daemon lacks network_forward")
	}
	ctx := context.Background()
	network := forwardTestNetwork(t, b)

	fw := backend.NetworkForward{ListenAddress: "10.99.77.50", Description: "it-fwd", DefaultTarget: "10.99.77.10"}
	require.NoError(t, b.CreateNetworkForward(ctx, network, fw))
	t.Cleanup(func() { _ = b.DeleteNetworkForward(ctx, network, fw.ListenAddress) })

	fws, err := b.ListNetworkForwards(ctx, network)
	require.NoError(t, err)
	require.Len(t, fws, 1)
	require.Equal(t, "it-fwd", fws[0].Description)
	require.Equal(t, "10.99.77.10", fws[0].DefaultTarget)

	// Update replaces the port set (the daemon enforces no etag on forwards).
	got := fws[0]
	got.Ports = []backend.ForwardPort{{Protocol: "tcp", ListenPort: "8080", TargetAddress: "10.99.77.11", TargetPort: "80"}}
	require.NoError(t, b.UpdateNetworkForward(ctx, network, got))

	fws, err = b.ListNetworkForwards(ctx, network)
	require.NoError(t, err)
	require.Len(t, fws[0].Ports, 1)

	require.NoError(t, b.DeleteNetworkForward(ctx, network, fw.ListenAddress))
	err = b.DeleteNetworkForward(ctx, network, fw.ListenAddress)
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestNetworkLeasesAndStateIntegration(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	network := forwardTestNetwork(t, b)

	// A fresh bridge has no tenant leases; the call itself must succeed.
	leases, err := b.ListNetworkLeases(ctx, network)
	require.NoError(t, err)
	for _, l := range leases {
		require.NotEqual(t, "dynamic", l.Type, "fresh bridge can't have tenant leases")
	}

	st, err := b.GetNetworkState(ctx, network)
	require.NoError(t, err)
	require.NotZero(t, st.MTU)
}
