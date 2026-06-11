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
// editor: volatile.* (internal/auto-managed), limits.cpu/limits.memory (owned by
// the Limits form), and snapshots.* (owned by the snapshot-schedule form). These
// are hidden from the editor and preserved on update.
func managedConfigKey(k string) bool {
	return strings.HasPrefix(k, "volatile.") ||
		k == "limits.cpu" || k == "limits.memory" ||
		k == "snapshots.schedule" || k == "snapshots.expiry" || k == "snapshots.pattern"
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

func (b *incusBackend) GetInstanceConfig(ctx context.Context, name string) (backend.InstanceConfig, error) {
	inst, etag, err := b.project(ctx).GetInstance(name)
	if err != nil {
		return backend.InstanceConfig{}, fmt.Errorf("get instance %q: %w", name, mapErr(err))
	}
	return backend.InstanceConfig{
		Config:       editableConfig(inst.Config),
		Devices:      inst.ExpandedDevices,
		LocalDevices: inst.Devices,
		Version:      etag,
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

// AddDevice attaches or overwrites a local device (GET-then-PUT, like UpdateLimits).
func (b *incusBackend) AddDevice(ctx context.Context, name, device string, config map[string]string) error {
	return b.mutateInstance(ctx, name, func(put *api.InstancePut) {
		if put.Devices == nil {
			put.Devices = map[string]map[string]string{}
		}
		put.Devices[device] = config
	}, "add device %q to %q", device, name)
}

// UpdateDevice replaces an existing local device's config map. The version is
// the etag from GetInstanceConfig: the daemon rejects the PUT with 412 (mapped
// to ErrConflict) when the instance changed since that read. An empty version
// sends the fresh GET's etag, updating unconditionally in practice.
func (b *incusBackend) UpdateDevice(ctx context.Context, name, device string, config map[string]string, version string) error {
	inst, etag, err := b.project(ctx).GetInstance(name)
	if err != nil {
		return fmt.Errorf("get instance %q: %w", name, mapErr(err))
	}
	put := inst.Writable()
	if _, ok := put.Devices[device]; !ok {
		return fmt.Errorf("device %q on %q: %w", device, name, backend.ErrNotFound)
	}
	put.Devices[device] = config
	if version == "" {
		version = etag
	}
	op, err := b.project(ctx).UpdateInstance(name, put, version)
	return waitOp(ctx, op, err, "update device %q on %q", device, name)
}

// RemoveDevice detaches a local device. Absent devices 404 before any PUT.
func (b *incusBackend) RemoveDevice(ctx context.Context, name, device string) error {
	inst, etag, err := b.project(ctx).GetInstance(name)
	if err != nil {
		return fmt.Errorf("get instance %q: %w", name, mapErr(err))
	}
	put := inst.Writable()
	if _, ok := put.Devices[device]; !ok {
		return fmt.Errorf("device %q on %q: %w", device, name, backend.ErrNotFound)
	}
	delete(put.Devices, device)
	op, err := b.project(ctx).UpdateInstance(name, put, etag)
	return waitOp(ctx, op, err, "remove device %q from %q", device, name)
}

func setOrDelete(m map[string]string, key, val string) {
	if val == "" {
		delete(m, key)
		return
	}
	m[key] = val
}
