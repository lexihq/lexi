package fake

import (
	"context"
	"maps"

	"github.com/adam/lxcon/internal/backend"
)

func (f *Fake) GetInstanceConfig(_ context.Context, name string) (backend.InstanceConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return backend.InstanceConfig{}, notFound(name)
	}
	cfg := maps.Clone(in.config)
	// Read-only devices = merge of the instance's assigned profiles' devices.
	devices := map[string]map[string]string{}
	for _, pn := range in.Profiles {
		p, ok := f.profiles[pn]
		if !ok {
			continue
		}
		for devName, dev := range p.Devices {
			devices[devName] = maps.Clone(dev)
		}
	}
	return backend.InstanceConfig{Config: cfg, Devices: devices}, nil
}

func (f *Fake) UpdateInstanceConfig(_ context.Context, name string, config map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	in.config = maps.Clone(config)
	return nil
}

func (f *Fake) UpdateLimits(_ context.Context, name string, l backend.Limits) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	in.LimitsCPU = l.CPU
	in.LimitsMemory = l.Memory
	return nil
}
