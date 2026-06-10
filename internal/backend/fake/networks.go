package fake

import (
	"context"
	"maps"
	"sort"
	"strconv"

	"github.com/adam/lxcon/internal/backend"
)

func (f *Fake) ListNetworks(_ context.Context) ([]backend.Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]backend.Network, 0, len(f.networks))
	for name := range f.networks {
		out = append(out, f.networkView(name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetNetwork(_ context.Context, name string) (backend.Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.networks[name]; !ok {
		return backend.Network{}, notFoundf("network %q", name)
	}
	n := f.networkView(name)
	n.Version = strconv.Itoa(f.networkVersions[name])
	return n, nil
}

func (f *Fake) CreateNetwork(_ context.Context, n backend.Network) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.networks[n.Name]; ok {
		return conflict("network %q already exists", n.Name)
	}
	f.networks[n.Name] = backend.Network{
		Name: n.Name, Type: n.Type, Managed: true,
		Description: n.Description, Config: maps.Clone(n.Config),
	}
	return nil
}

func (f *Fake) UpdateNetwork(_ context.Context, name, description string, config map[string]string, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	net, ok := f.networks[name]
	if !ok {
		return notFoundf("network %q", name)
	}
	if !net.Managed {
		return invalid("network %q is unmanaged", name)
	}
	// Empty version = unconditional, mirroring UpdateServerConfig; a stale
	// version means a concurrent writer landed first.
	if version != "" && version != strconv.Itoa(f.networkVersions[name]) {
		return conflict("network %q version %s", name, version)
	}
	net.Description = description
	net.Config = maps.Clone(config)
	if net.Config == nil {
		net.Config = map[string]string{}
	}
	f.networks[name] = net
	f.networkVersions[name]++
	return nil
}

func (f *Fake) DeleteNetwork(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	net, ok := f.networks[name]
	if !ok {
		return notFoundf("network %q", name)
	}
	if !net.Managed {
		return invalid("network %q is unmanaged", name)
	}
	// Incus parity: a network referenced by an instance cannot be deleted.
	for _, in := range f.instances {
		if f.instanceUsesNetwork(in, name) {
			return invalid("network %q is in use", name)
		}
	}
	delete(f.networks, name)
	delete(f.networkVersions, name)
	return nil
}

// networkView returns a copy with a fresh UsedBy derived from instances whose
// (expanded) nic devices reference the network. Callers must hold the mutex.
func (f *Fake) networkView(name string) backend.Network {
	n := f.networks[name]
	n.Config = maps.Clone(n.Config)
	var usedBy []string
	for instName, in := range f.instances {
		if f.instanceUsesNetwork(in, name) {
			usedBy = append(usedBy, instName)
		}
	}
	sort.Strings(usedBy)
	n.UsedBy = usedBy
	return n
}

func (f *Fake) instanceUsesNetwork(in *instance, network string) bool {
	hasNic := func(devs map[string]map[string]string) bool {
		for _, d := range devs {
			if d["type"] == "nic" && d["network"] == network {
				return true
			}
		}
		return false
	}
	if hasNic(in.devices) {
		return true
	}
	for _, pn := range in.Profiles {
		if p, ok := f.profiles[pn]; ok && hasNic(p.Devices) {
			return true
		}
	}
	return false
}
