package fake

import (
	"context"
	"maps"
	"strconv"

	"github.com/lexihq/lexi/internal/backend"
)

func (f *Fake) GetInstanceConfig(ctx context.Context, name string) (backend.InstanceConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return backend.InstanceConfig{}, notFound(name)
	}
	// Expanded = profile devices, then local devices win on name collision.
	expanded := map[string]map[string]string{}
	for _, pn := range in.Profiles {
		p, ok := sp.profiles[pn]
		if !ok {
			continue
		}
		for devName, dev := range p.Devices {
			expanded[devName] = maps.Clone(dev)
		}
	}
	for devName, dev := range in.devices {
		expanded[devName] = maps.Clone(dev)
	}
	return backend.InstanceConfig{
		Config:       editableConfig(in.config),
		Devices:      expanded,
		LocalDevices: cloneDevices(in.devices),
		Version:      backend.Version(strconv.Itoa(in.configVersion)),
	}, nil
}

// editableConfig returns the user-editable subset of the stored config (a
// copy), excluding the managed keys.
func editableConfig(local map[string]string) map[string]string {
	out := make(map[string]string, len(local))
	for k, v := range local {
		if backend.ManagedConfigKey(k) {
			continue
		}
		out[k] = v
	}
	return out
}

// UpdateInstanceConfig mirrors the incus driver: managed keys (volatile.*,
// limits, snapshots.*) are preserved from the stored config and ignored on
// input, so saving the editor can't wipe a snapshot schedule. A non-empty
// stale version conflicts, same as UpdateDevice.
func (f *Fake) UpdateInstanceConfig(ctx context.Context, name string, config map[string]string, version backend.Version) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	if version != "" && string(version) != strconv.Itoa(in.configVersion) {
		return conflict("instance %q config version %s", name, version)
	}
	next := map[string]string{}
	for k, v := range in.config {
		if backend.ManagedConfigKey(k) {
			next[k] = v
		}
	}
	for k, v := range config {
		if backend.ManagedConfigKey(k) {
			continue
		}
		next[k] = v
	}
	in.config = next
	in.configVersion++
	return nil
}

func (f *Fake) AddDevice(ctx context.Context, name, device string, config map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	if in.devices == nil { // clone/import don't seed the map
		in.devices = map[string]map[string]string{}
	}
	in.devices[device] = maps.Clone(config)
	in.configVersion++
	return nil
}

func (f *Fake) UpdateDevice(ctx context.Context, name, device string, config map[string]string, version backend.Version) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	if _, ok := in.devices[device]; !ok {
		return notFoundf("device %q on %q", device, name)
	}
	// Empty version = unconditional, mirroring UpdateServerConfig; a stale
	// version means a concurrent writer landed first.
	if version != "" && string(version) != strconv.Itoa(in.configVersion) {
		return conflict("instance %q config version %s", name, version)
	}
	in.devices[device] = maps.Clone(config)
	in.configVersion++
	return nil
}

func (f *Fake) RemoveDevice(ctx context.Context, name, device string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	if _, ok := in.devices[device]; !ok {
		return notFoundf("device %q on %q", device, name)
	}
	delete(in.devices, device)
	in.configVersion++
	return nil
}

func (f *Fake) UpdateLimits(ctx context.Context, name string, l backend.Limits) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	in.LimitsCPU = l.CPU
	in.LimitsMemory = l.Memory
	// Incus PUTs the whole instance, changing its ETag; mirror that so stale
	// config version tokens conflict here the same way they do in production.
	in.configVersion++
	return nil
}
