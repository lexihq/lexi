package fake

import (
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkCRUD(t *testing.T) {
	f := New()

	nets, err := f.ListNetworks(ctx())
	require.NoError(t, err)
	assert.NotEmpty(t, nets) // seeded incusbr0 + eth0

	br, err := f.GetNetwork(ctx(), "incusbr0")
	require.NoError(t, err)
	assert.True(t, br.Managed)
	assert.Equal(t, "bridge", br.Type)

	require.NoError(t, f.CreateNetwork(ctx(), backend.Network{Name: "br1", Type: "bridge", Config: map[string]string{"ipv4.nat": "true"}}))
	br1, err := f.GetNetwork(ctx(), "br1")
	require.NoError(t, err)
	assert.True(t, br1.Managed)
	assert.Equal(t, "true", br1.Config["ipv4.nat"])

	require.ErrorIs(t, f.CreateNetwork(ctx(), backend.Network{Name: "br1", Type: "bridge"}), backend.ErrConflict)

	require.NoError(t, f.DeleteNetwork(ctx(), "br1"))
	_, err = f.GetNetwork(ctx(), "br1")
	require.ErrorIs(t, err, backend.ErrNotFound)

	require.ErrorIs(t, f.DeleteNetwork(ctx(), "missing"), backend.ErrNotFound)
	require.ErrorIs(t, f.DeleteNetwork(ctx(), "eth0"), backend.ErrInvalid) // unmanaged
}

func TestNetworkUsedByDerivedFromNic(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo"}))
	// demo gets the default profile, whose eth0 nic uses incusbr0.
	br, err := f.GetNetwork(ctx(), "incusbr0")
	require.NoError(t, err)
	assert.Contains(t, br.UsedBy, "demo")
}
