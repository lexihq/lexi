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
		Config:       maps.Clone(in.config),
		Devices:      expanded,
		LocalDevices: maps.Clone(in.devices),
		Version:      strconv.Itoa(in.configVersion),
	}, nil
}

func (f *Fake) UpdateInstanceConfig(ctx context.Context, name string, config map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	in.config = maps.Clone(config)
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

func (f *Fake) UpdateDevice(ctx context.Context, name, device string, config map[string]string, version string) error {
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
	if version != "" && version != strconv.Itoa(in.configVersion) {
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
	return nil
}
