package fake

import (
	"context"
	"maps"
	"sort"

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
	return f.networkView(name), nil
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
	delete(f.networks, name)
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
