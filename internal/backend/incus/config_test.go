package incus

import (
	"context"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEditableConfigDropsVolatileAndLimits(t *testing.T) {
	got := editableConfig(map[string]string{
		"security.nesting":     "true",
		"boot.autostart":       "1",
		"volatile.eth0.hwaddr": "00:16:3e:aa:bb:cc",
		"limits.cpu":           "2",
		"limits.memory":        "2GiB",
	})
	assert.Equal(t, map[string]string{"security.nesting": "true", "boot.autostart": "1"}, got)
}

func TestGetInstanceConfigFiltersAndCarriesDevices(t *testing.T) {
	srv := &instanceServerStub{instance: &api.Instance{
		InstancePut: api.InstancePut{Config: map[string]string{
			"security.nesting":    "true",
			"volatile.base_image": "abc",
			"limits.cpu":          "2",
		}},
		ExpandedDevices: map[string]map[string]string{"root": {"type": "disk", "path": "/"}},
	}}
	b := &incusBackend{srv: srv}
	cfg, err := b.GetInstanceConfig(context.Background(), "demo")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"security.nesting": "true"}, cfg.Config)
	assert.Equal(t, "disk", cfg.Devices["root"]["type"])
}

func TestGetInstanceConfigSeparatesLocalDevices(t *testing.T) {
	srv := &instanceServerStub{instance: &api.Instance{
		InstancePut: api.InstancePut{Devices: map[string]map[string]string{
			"data": {"type": "disk", "source": "/mnt/x"},
		}},
		ExpandedDevices: map[string]map[string]string{
			"root": {"type": "disk", "path": "/"},
			"data": {"type": "disk", "source": "/mnt/x"},
		},
	}}
	b := &incusBackend{srv: srv}
	cfg, err := b.GetInstanceConfig(context.Background(), "demo")
	require.NoError(t, err)
	assert.Contains(t, cfg.LocalDevices, "data")
	assert.NotContains(t, cfg.LocalDevices, "root") // root is inherited
	assert.Contains(t, cfg.Devices, "root")         // expanded carries both
}

func TestUpdateInstanceConfigPreservesVolatileAndLimits(t *testing.T) {
	srv := &instanceServerStub{instance: &api.Instance{
		InstancePut: api.InstancePut{Config: map[string]string{
			"security.nesting":    "true", // old editable key, should be dropped
			"volatile.base_image": "abc",  // preserved
			"limits.cpu":          "2",    // preserved
		}},
	}}
	b := &incusBackend{srv: srv}
	require.NoError(t, b.UpdateInstanceConfig(context.Background(), "demo",
		map[string]string{"boot.autostart": "1"}, ""))
	require.NotNil(t, srv.updatedPut)
	assert.Equal(t, api.ConfigMap{
		"boot.autostart":      "1",
		"volatile.base_image": "abc",
		"limits.cpu":          "2",
	}, srv.updatedPut.Config)
}

func TestAddDevicePutsDevice(t *testing.T) {
	srv := &instanceServerStub{instance: &api.Instance{}}
	b := &incusBackend{srv: srv}
	require.NoError(t, b.AddDevice(context.Background(), "demo", "web",
		map[string]string{"type": "proxy", "listen": "tcp:0.0.0.0:80"}))
	require.NotNil(t, srv.updatedPut)
	assert.Equal(t, "proxy", srv.updatedPut.Devices["web"]["type"])
}

func TestRemoveDeviceDeletesDevice(t *testing.T) {
	srv := &instanceServerStub{instance: &api.Instance{
		InstancePut: api.InstancePut{Devices: map[string]map[string]string{
			"web": {"type": "proxy"},
		}},
	}}
	b := &incusBackend{srv: srv}
	require.NoError(t, b.RemoveDevice(context.Background(), "demo", "web"))
	require.NotNil(t, srv.updatedPut)
	assert.NotContains(t, srv.updatedPut.Devices, "web")
}

func TestRemoveDeviceAbsentIsNotFoundNoPut(t *testing.T) {
	srv := &instanceServerStub{instance: &api.Instance{}}
	b := &incusBackend{srv: srv}
	err := b.RemoveDevice(context.Background(), "demo", "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)
	assert.Nil(t, srv.updatedPut) // no write issued
}
