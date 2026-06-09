package incus

import (
	"context"
	"testing"

	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListProfilesMapsFields(t *testing.T) {
	srv := &instanceServerStub{profiles: []api.Profile{
		{Name: "default", ProfilePut: api.ProfilePut{
			Description: "d", Config: map[string]string{"k": "v"},
			Devices: map[string]map[string]string{"eth0": {"type": "nic"}}},
			UsedBy: []string{"/1.0/instances/c1"}},
	}}
	b := &incusBackend{srv: srv}
	got, err := b.ListProfiles(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "default", got[0].Name)
	assert.Equal(t, "v", got[0].Config["k"])
	assert.Equal(t, "nic", got[0].Devices["eth0"]["type"])
	assert.Equal(t, []string{"/1.0/instances/c1"}, got[0].UsedBy)
}

func TestSetInstanceProfilesGetThenPut(t *testing.T) {
	srv := &instanceServerStub{
		instance: &api.Instance{Name: "demo",
			InstancePut: api.InstancePut{Profiles: []string{"default"}}},
	}
	b := &incusBackend{srv: srv}
	require.NoError(t, b.SetInstanceProfiles(context.Background(), "demo", []string{"default", "gpu"}))
	require.NotNil(t, srv.updatedPut)
	assert.Equal(t, []string{"default", "gpu"}, srv.updatedPut.Profiles)
}
