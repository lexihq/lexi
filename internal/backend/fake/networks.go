package fake

import (
	"context"
	"maps"
	"sort"
	"strconv"

	"github.com/lexihq/lexi/internal/backend"
)

func (f *Fake) ListNetworks(ctx context.Context) ([]backend.Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.networkSpace(ctx)

	out := make([]backend.Network, 0, len(sp.networks))
	for name := range sp.networks {
		out = append(out, f.networkView(ctx, sp, name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetNetwork(ctx context.Context, name string) (backend.Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.networkSpace(ctx)

	if _, ok := sp.networks[name]; !ok {
		return backend.Network{}, notFoundf("network %q", name)
	}
	n := f.networkView(ctx, sp, name)
	n.Version = strconv.Itoa(sp.networkVersions[name])
	return n, nil
}

func (f *Fake) CreateNetwork(ctx context.Context, n backend.Network) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.networkSpace(ctx)

	if _, ok := sp.networks[n.Name]; ok {
		return conflict("network %q already exists", n.Name)
	}
	sp.networks[n.Name] = backend.Network{
		Name: n.Name, Type: n.Type, Managed: true, Status: "Created",
		Description: n.Description, Config: maps.Clone(n.Config),
	}
	return nil
}

func (f *Fake) UpdateNetwork(ctx context.Context, name, description string, config map[string]string, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.networkSpace(ctx)

	net, ok := sp.networks[name]
	if !ok {
		return notFoundf("network %q", name)
	}
	if !net.Managed {
		return invalid("network %q is unmanaged", name)
	}
	// Empty version = unconditional, mirroring UpdateServerConfig; a stale
	// version means a concurrent writer landed first.
	if version != "" && version != strconv.Itoa(sp.networkVersions[name]) {
		return conflict("network %q version %s", name, version)
	}
	net.Description = description
	net.Config = maps.Clone(config)
	if net.Config == nil {
		net.Config = map[string]string{}
	}
	sp.networks[name] = net
	sp.networkVersions[name]++
	return nil
}

func (f *Fake) DeleteNetwork(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.networkSpace(ctx)

	net, ok := sp.networks[name]
	if !ok {
		return notFoundf("network %q", name)
	}
	if !net.Managed {
		return invalid("network %q is unmanaged", name)
	}
	// Incus parity: a network referenced by an instance cannot be deleted.
	// Only projects sharing this network namespace count — isolated projects
	// can reuse the name freely.
	for _, in := range f.networkUsers(ctx) {
		if f.instanceUsesNetworkOf(ctx, in, name) {
			return invalid("network %q is in use", name)
		}
	}
	delete(sp.networks, name)
	delete(sp.networkVersions, name)
	return nil
}

// networkView returns a copy with a fresh UsedBy derived from instances whose
// (expanded) nic devices reference the network. Callers must hold the mutex.
func (f *Fake) networkView(ctx context.Context, owner *space, name string) backend.Network {
	n := owner.networks[name]
	n.Config = maps.Clone(n.Config)
	var usedBy []string
	for instName, in := range f.networkUsers(ctx) {
		if f.instanceUsesNetworkOf(ctx, in, name) {
			usedBy = append(usedBy, instName)
		}
	}
	sort.Strings(usedBy)
	n.UsedBy = usedBy
	return n
}

// networkUsers collects the instances of every project whose effective
// network namespace is the request's, keyed by instance name. Callers must
// hold the mutex.
func (f *Fake) networkUsers(ctx context.Context) map[string]*instance {
	owner := f.featureProject(ctx, "features.networks")
	out := map[string]*instance{}
	for project, spc := range f.remote(ctx).spaces {
		if f.remote(ctx).featureProjectName(project, "features.networks") != owner {
			continue
		}
		maps.Copy(out, spc.instances)
	}
	return out
}

// instanceUsesNetworkOf is instanceUsesNetwork with the instance's profile
// devices resolved through its own project's profile namespace.
func (f *Fake) instanceUsesNetworkOf(ctx context.Context, in *instance, network string) bool {
	return instanceUsesNetwork(f.featureSpace(ctx, "features.profiles"), in, network)
}

func instanceUsesNetwork(sp *space, in *instance, network string) bool {
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
		if p, ok := sp.profiles[pn]; ok && hasNic(p.Devices) {
			return true
		}
	}
	return false
}
