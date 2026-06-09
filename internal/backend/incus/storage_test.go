package incus

import (
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToPool(t *testing.T) {
	p := &api.StoragePool{Name: "default", Driver: "dir", UsedBy: []string{"/1.0/instances/c1"}}
	p.Description = "Default pool"
	p.Config = map[string]string{"source": "/var/lib/incus"}
	got := toPool(p)
	assert.Equal(t, "default", got.Name)
	assert.Equal(t, "dir", got.Driver)
	assert.Equal(t, "Default pool", got.Description)
	assert.Equal(t, "/var/lib/incus", got.Config["source"])
	assert.Equal(t, []string{"/1.0/instances/c1"}, got.UsedBy)
}

func TestToVolume(t *testing.T) {
	v := &api.StorageVolume{Name: "vol1", Type: "custom", ContentType: "filesystem", UsedBy: []string{"/1.0/instances/c1"}}
	v.Config = map[string]string{"size": "1GiB"}
	got := toVolume(v)
	assert.Equal(t, "vol1", got.Name)
	assert.Equal(t, "custom", got.Type)
	assert.Equal(t, "filesystem", got.ContentType)
	assert.Equal(t, "1GiB", got.Config["size"])
	assert.Equal(t, []string{"/1.0/instances/c1"}, got.UsedBy)
}

func TestListVolumesFiltersCustom(t *testing.T) {
	b := &incusBackend{srv: &instanceServerStub{volumes: []api.StorageVolume{
		{Name: "vol1", Type: "custom"},
		{Name: "c1", Type: "container"},
	}}}
	vols, err := b.ListVolumes(t.Context(), "default")
	require.NoError(t, err)
	require.Len(t, vols, 1)
	assert.Equal(t, "vol1", vols[0].Name)
}

func TestCreateVolumeSendsPost(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	err := b.CreateVolume(t.Context(), "default", backend.StorageVolume{
		Name: "vol1", ContentType: "filesystem", Config: map[string]string{"size": "1GiB"},
	})
	require.NoError(t, err)
	require.NotNil(t, s.createdVol)
	assert.Equal(t, "vol1", s.createdVol.Name)
	assert.Equal(t, "custom", s.createdVol.Type)
	assert.Equal(t, "filesystem", s.createdVol.ContentType)
	assert.Equal(t, "1GiB", s.createdVol.Config["size"])
}

func TestDeleteVolumeCallsThrough(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.DeleteVolume(t.Context(), "default", "vol1"))
	assert.Equal(t, [3]string{"default", "custom", "vol1"}, s.deletedVol)
}
