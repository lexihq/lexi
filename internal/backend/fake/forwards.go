package fake

import (
	"context"
	"fmt"
	"sort"

	"github.com/lexihq/lexi/internal/backend"
)

// ListNetworkLeases derives DHCP leases from the running instances whose NIC
// devices (expanded through profiles) attach to the network — the same
// shortcut the fake's UsedBy scanning takes.
func (f *Fake) ListNetworkLeases(ctx context.Context, network string) ([]backend.NetworkLease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	nsp := f.networkSpace(ctx)

	if _, ok := nsp.networks[network]; !ok {
		return nil, notFoundf("network %q", network)
	}
	sp := f.space(ctx)
	var leases []backend.NetworkLease
	for name, in := range sp.instances {
		if in.Status != "Running" || len(in.IPv4) == 0 {
			continue
		}
		if !f.instanceOnNetwork(sp, in, network) {
			continue
		}
		leases = append(leases, backend.NetworkLease{
			Hostname: name,
			MAC:      fakeMAC(name),
			Address:  in.IPv4[0],
			Type:     "dynamic",
		})
	}
	sort.Slice(leases, func(i, j int) bool { return leases[i].Hostname < leases[j].Hostname })
	return leases, nil
}

// instanceOnNetwork reports whether the instance has a NIC on the network,
// checking local devices first, then assigned profiles. Callers must hold
// the mutex.
func (f *Fake) instanceOnNetwork(sp *space, in *instance, network string) bool {
	for _, dev := range in.devices {
		if dev["type"] == "nic" && dev["network"] == network {
			return true
		}
	}
	for _, profName := range in.Profiles {
		prof, ok := sp.profiles[profName]
		if !ok {
			continue
		}
		for _, dev := range prof.Devices {
			if dev["type"] == "nic" && dev["network"] == network {
				return true
			}
		}
	}
	// Parity with the fake's create default: no NICs anywhere means the
	// default profile's bridge.
	return len(in.Profiles) == 0 && len(in.devices) == 0 && network == "incusbr0"
}

// fakeMAC derives a stable fake hardware address from the instance name.
func fakeMAC(name string) string {
	var sum byte
	for i := range len(name) {
		sum += name[i]
	}
	return fmt.Sprintf("10:66:6a:00:%02x:%02x", len(name)%256, sum)
}

// GetNetworkState reports a synthesized live state for a managed network.
func (f *Fake) GetNetworkState(ctx context.Context, network string) (backend.NetworkState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	nsp := f.networkSpace(ctx)

	n, ok := nsp.networks[network]
	if !ok {
		return backend.NetworkState{}, notFoundf("network %q", network)
	}
	st := backend.NetworkState{State: "up", MTU: 1500}
	if addr := n.Config["ipv4.address"]; addr != "" {
		st.Addresses = []string{addr}
	}
	return st, nil
}

func (f *Fake) ListNetworkForwards(ctx context.Context, network string) ([]backend.NetworkForward, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	nsp := f.networkSpace(ctx)

	if _, ok := nsp.networks[network]; !ok {
		return nil, notFoundf("network %q", network)
	}
	var out []backend.NetworkForward
	for _, fw := range nsp.forwards[network] {
		fw.Ports = append([]backend.ForwardPort(nil), fw.Ports...)
		out = append(out, fw)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ListenAddress < out[j].ListenAddress })
	return out, nil
}

func (f *Fake) CreateNetworkForward(ctx context.Context, network string, fw backend.NetworkForward) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	nsp := f.networkSpace(ctx)

	if _, ok := nsp.networks[network]; !ok {
		return notFoundf("network %q", network)
	}
	if fw.ListenAddress == "" {
		return invalid("listen address is required")
	}
	if _, ok := nsp.forwards[network][fw.ListenAddress]; ok {
		return conflict("forward %q already exists", fw.ListenAddress)
	}
	if nsp.forwards[network] == nil {
		nsp.forwards[network] = map[string]backend.NetworkForward{}
	}
	nsp.forwards[network][fw.ListenAddress] = fw
	f.logOp(f.space(ctx), fmt.Sprintf("Creating network forward %q on %q", fw.ListenAddress, network))
	return nil
}

func (f *Fake) UpdateNetworkForward(ctx context.Context, network string, fw backend.NetworkForward) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	nsp := f.networkSpace(ctx)

	if _, ok := nsp.forwards[network][fw.ListenAddress]; !ok {
		return notFoundf("forward %q", fw.ListenAddress)
	}
	nsp.forwards[network][fw.ListenAddress] = fw
	f.logOp(f.space(ctx), fmt.Sprintf("Updating network forward %q on %q", fw.ListenAddress, network))
	return nil
}

func (f *Fake) DeleteNetworkForward(ctx context.Context, network, listenAddress string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	nsp := f.networkSpace(ctx)

	if _, ok := nsp.forwards[network][listenAddress]; !ok {
		return notFoundf("forward %q", listenAddress)
	}
	delete(nsp.forwards[network], listenAddress)
	f.logOp(f.space(ctx), fmt.Sprintf("Deleting network forward %q on %q", listenAddress, network))
	return nil
}
