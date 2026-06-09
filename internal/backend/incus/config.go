package incus

import (
	"context"
	"fmt"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

// UpdateLimits sets or clears limits.cpu/limits.memory on the instance's local
// config (GET-then-PUT, matching RestoreSnapshot). Empty values delete the key.
func (b *incusBackend) UpdateLimits(ctx context.Context, name string, l backend.Limits) error {
	return b.mutateInstance(ctx, name, func(put *api.InstancePut) {
		if put.Config == nil {
			put.Config = map[string]string{}
		}
		setOrDelete(put.Config, "limits.cpu", l.CPU)
		setOrDelete(put.Config, "limits.memory", l.Memory)
	}, "update limits on %q", name)
}

// managedConfigKey reports whether a config key is managed outside the config
// editor: volatile.* (internal/auto-managed) and limits.cpu/limits.memory (owned
// by the Limits form). These are hidden from the editor and preserved on update.
func managedConfigKey(k string) bool {
	return strings.HasPrefix(k, "volatile.") || k == "limits.cpu" || k == "limits.memory"
}

// editableConfig returns the user-editable subset of an instance's local config
// (a copy), excluding the managed keys.
func editableConfig(local map[string]string) map[string]string {
	out := make(map[string]string, len(local))
	for k, v := range local {
		if managedConfigKey(k) {
			continue
		}
		out[k] = v
	}
	return out
}

func (b *incusBackend) GetInstanceConfig(_ context.Context, name string) (backend.InstanceConfig, error) {
	inst, _, err := b.srv.GetInstance(name)
	if err != nil {
		return backend.InstanceConfig{}, fmt.Errorf("get instance %q: %w", name, mapErr(err))
	}
	return backend.InstanceConfig{
		Config:       editableConfig(inst.Config),
		Devices:      inst.ExpandedDevices,
		LocalDevices: inst.Devices,
	}, nil
}

// UpdateInstanceConfig replaces the editable local config (GET-then-PUT, like
// UpdateLimits), preserving the managed keys and ignoring any a client tries to
// set through the editor.
func (b *incusBackend) UpdateInstanceConfig(ctx context.Context, name string, config map[string]string) error {
	return b.mutateInstance(ctx, name, func(put *api.InstancePut) {
		next := map[string]string{}
		for k, v := range put.Config {
			if managedConfigKey(k) {
				next[k] = v
			}
		}
		for k, v := range config {
			if managedConfigKey(k) {
				continue
			}
			next[k] = v
		}
		put.Config = next
	}, "update config on %q", name)
}

func setOrDelete(m map[string]string, key, val string) {
	if val == "" {
		delete(m, key)
		return
	}
	m[key] = val
}
