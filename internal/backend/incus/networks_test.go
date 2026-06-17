package incus

import (
	"context"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToNetworkMapsFields(t *testing.T) {
	n := &api.Network{Name: "br0", Type: "bridge", Managed: true, UsedBy: []string{"/1.0/instances/c1"}}
	n.Config = map[string]string{"ipv4.nat": "true"}
	n.Description = "d"
	got := toNetwork(n)
	assert.Equal(t, "br0", got.Name)
	assert.True(t, got.Managed)
	assert.Equal(t, "true", got.Config["ipv4.nat"])
	assert.Equal(t, []string{"/1.0/instances/c1"}, got.UsedBy)
}

func TestCreateNetworkSendsPost(t *testing.T) {
	srv := &instanceServerStub{}
	b := &incusBackend{srv: srv}
	require.NoError(t, b.CreateNetwork(context.Background(), backend.Network{
		Name: "br1", Type: "bridge", Description: "d", Config: map[string]string{"ipv4.nat": "true"},
	}))
	require.NotNil(t, srv.createdNet)
	assert.Equal(t, "br1", srv.createdNet.Name)
	assert.Equal(t, "bridge", srv.createdNet.Type)
	assert.Equal(t, "true", srv.createdNet.Config["ipv4.nat"])
}

func TestDeleteNetworkCallsThrough(t *testing.T) {
	srv := &instanceServerStub{}
	b := &incusBackend{srv: srv}
	require.NoError(t, b.DeleteNetwork(context.Background(), "br1"))
	assert.Equal(t, "br1", srv.deletedNet)
}
