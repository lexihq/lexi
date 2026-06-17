package fake

import (
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkForwardLifecycle(t *testing.T) {
	f := New()

	require.NoError(t, f.CreateNetworkForward(ctx(), "incusbr0", backend.NetworkForward{
		ListenAddress: "192.0.2.10", Description: "web", DefaultTarget: "10.0.3.2",
	}))

	// Duplicate listen address conflicts; unknown network is not found.
	err := f.CreateNetworkForward(ctx(), "incusbr0", backend.NetworkForward{ListenAddress: "192.0.2.10"})
	require.ErrorIs(t, err, backend.ErrConflict)
	err = f.CreateNetworkForward(ctx(), "ghost0", backend.NetworkForward{ListenAddress: "192.0.2.11"})
	require.ErrorIs(t, err, backend.ErrNotFound)

	fws, err := f.ListNetworkForwards(ctx(), "incusbr0")
	require.NoError(t, err)
	require.Len(t, fws, 1)
	assert.Equal(t, "web", fws[0].Description)
	assert.Equal(t, "10.0.3.2", fws[0].DefaultTarget)

	// Update replaces the port set (unversioned, like the daemon).
	fw := fws[0]
	fw.Ports = []backend.ForwardPort{{Protocol: "tcp", ListenPort: "80", TargetAddress: "10.0.3.2", TargetPort: "8080"}}
	require.NoError(t, f.UpdateNetworkForward(ctx(), "incusbr0", fw))

	fws, err = f.ListNetworkForwards(ctx(), "incusbr0")
	require.NoError(t, err)
	require.Len(t, fws[0].Ports, 1)
	assert.Equal(t, "8080", fws[0].Ports[0].TargetPort)

	require.NoError(t, f.DeleteNetworkForward(ctx(), "incusbr0", "192.0.2.10"))
	err = f.DeleteNetworkForward(ctx(), "incusbr0", "192.0.2.10")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestNetworkLeasesDeriveFromRunningInstances(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "leasy", Image: "debian/12"}))

	// Stopped instances hold no lease.
	leases, err := f.ListNetworkLeases(ctx(), "incusbr0")
	require.NoError(t, err)
	assert.Empty(t, leases)

	require.NoError(t, f.StartInstance(ctx(), "leasy"))
	leases, err = f.ListNetworkLeases(ctx(), "incusbr0")
	require.NoError(t, err)
	require.Len(t, leases, 1)
	assert.Equal(t, "leasy", leases[0].Hostname)
	assert.NotEmpty(t, leases[0].Address)
	assert.Equal(t, "dynamic", leases[0].Type)

	st, err := f.GetNetworkState(ctx(), "incusbr0")
	require.NoError(t, err)
	assert.Equal(t, "up", st.State)
	assert.NotZero(t, st.MTU)
}
