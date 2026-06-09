package incus

import (
	"context"
	"fmt"
	"strings"

	"github.com/adam/lxcon/internal/backend"
)

// UpdateLimits sets or clears limits.cpu/limits.memory on the instance's local
// config (GET-then-PUT, matching RestoreSnapshot). Empty values delete the key.
func (b *incusBackend) UpdateLimits(ctx context.Context, name string, l backend.Limits) error {
	inst, etag, err := b.srv.GetInstance(name)
	if err != nil {
		return fmt.Errorf("get instance %q: %w", name, mapErr(err))
	}
	put := inst.Writable()
	if put.Config == nil {
		put.Config = map[string]string{}
	}
	setOrDelete(put.Config, "limits.cpu", l.CPU)
	setOrDelete(put.Config, "limits.memory", l.Memory)

	op, err := b.srv.UpdateInstance(name, put, etag)
	if err != nil {
		return fmt.Errorf("update limits on %q: %w", name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("update limits on %q: %w", name, mapErr(err))
	}
	return nil
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
		Config:  editableConfig(inst.Config),
		Devices: inst.ExpandedDevices,
	}, nil
}

// UpdateInstanceConfig replaces the editable local config (GET-then-PUT, like
// UpdateLimits), preserving the managed keys and ignoring any a client tries to
// set through the editor.
func (b *incusBackend) UpdateInstanceConfig(ctx context.Context, name string, config map[string]string) error {
	inst, etag, err := b.srv.GetInstance(name)
	if err != nil {
		return fmt.Errorf("get instance %q: %w", name, mapErr(err))
	}
	put := inst.Writable()
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
	op, err := b.srv.UpdateInstance(name, put, etag)
	if err != nil {
		return fmt.Errorf("update config on %q: %w", name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("update config on %q: %w", name, mapErr(err))
	}
	return nil
}

func setOrDelete(m map[string]string, key, val string) {
	if val == "" {
		delete(m, key)
		return
	}
	m[key] = val
}
