package incus

import (
	"context"
	"fmt"
	"strings"

	"github.com/lxc/incus/v6/shared/api"

	"github.com/lexihq/lexi/internal/backend"
)

// ListNetworkLeases reports a managed network's DHCP leases, gateway entries
// excluded — the UI shows tenant addresses, not the bridge's own.
func (b *incusBackend) ListNetworkLeases(ctx context.Context, network string) ([]backend.NetworkLease, error) {
	leases, err := b.project(ctx).GetNetworkLeases(network)
	if err != nil {
		return nil, fmt.Errorf("list leases for %q: %w", network, mapErr(err))
	}
	out := make([]backend.NetworkLease, 0, len(leases))
	for _, l := range leases {
		// Gateway entries are the bridge's own address, not a tenant lease; the
		// UI shows tenant addresses only (as documented above and in the fake).
		if l.Type == "gateway" {
			continue
		}
		out = append(out, backend.NetworkLease{
			Hostname: l.Hostname,
			MAC:      l.Hwaddr,
			Address:  l.Address,
			Type:     l.Type,
		})
	}
	return out, nil
}

func (b *incusBackend) GetNetworkState(ctx context.Context, network string) (backend.NetworkState, error) {
	st, err := b.project(ctx).GetNetworkState(network)
	if err != nil {
		return backend.NetworkState{}, fmt.Errorf("network state for %q: %w", network, mapErr(err))
	}
	out := backend.NetworkState{State: st.State, MTU: st.Mtu}
	for _, a := range st.Addresses {
		out.Addresses = append(out.Addresses, a.Address+"/"+a.Netmask)
	}
	return out, nil
}

func (b *incusBackend) ListNetworkForwards(ctx context.Context, network string) ([]backend.NetworkForward, error) {
	fws, err := b.project(ctx).GetNetworkForwards(network)
	if err != nil {
		return nil, fmt.Errorf("list forwards for %q: %w", network, mapErr(err))
	}
	out := make([]backend.NetworkForward, 0, len(fws))
	for i := range fws {
		out = append(out, forwardView(fws[i]))
	}
	return out, nil
}

func forwardView(fw api.NetworkForward) backend.NetworkForward {
	out := backend.NetworkForward{
		ListenAddress: fw.ListenAddress,
		Description:   fw.Description,
		DefaultTarget: fw.Config["target_address"],
	}
	for _, p := range fw.Ports {
		out.Ports = append(out.Ports, backend.ForwardPort{
			Description:   p.Description,
			Protocol:      p.Protocol,
			ListenPort:    p.ListenPort,
			TargetAddress: p.TargetAddress,
			TargetPort:    p.TargetPort,
		})
	}
	return out
}

func forwardPut(fw backend.NetworkForward) api.NetworkForwardPut {
	put := api.NetworkForwardPut{
		Description: fw.Description,
		Config:      map[string]string{},
	}
	if fw.DefaultTarget != "" {
		put.Config["target_address"] = fw.DefaultTarget
	}
	for _, p := range fw.Ports {
		put.Ports = append(put.Ports, api.NetworkForwardPort{
			Description:   p.Description,
			Protocol:      p.Protocol,
			ListenPort:    p.ListenPort,
			TargetAddress: p.TargetAddress,
			TargetPort:    p.TargetPort,
		})
	}
	return put
}

func (b *incusBackend) CreateNetworkForward(ctx context.Context, network string, fw backend.NetworkForward) error {
	req := api.NetworkForwardsPost{
		NetworkForwardPut: forwardPut(fw),
		ListenAddress:     fw.ListenAddress,
	}
	if err := b.project(ctx).CreateNetworkForward(network, req); err != nil {
		// The daemon reports a duplicate as a plain 400, which mapErr's typed
		// BadRequest branch would turn into ErrInvalid before the string
		// fallback can see it (same patch as the zone/bucket/ACL creates).
		if strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("forward %q on %q already exists: %w", fw.ListenAddress, network, backend.ErrConflict)
		}
		return fmt.Errorf("create forward %q on %q: %w", fw.ListenAddress, network, mapErr(err))
	}
	return nil
}

func (b *incusBackend) UpdateNetworkForward(ctx context.Context, network string, fw backend.NetworkForward) error {
	// No etag: the daemon doesn't enforce one on forwards (last write wins).
	if err := b.project(ctx).UpdateNetworkForward(network, fw.ListenAddress, forwardPut(fw), ""); err != nil {
		return fmt.Errorf("update forward %q on %q: %w", fw.ListenAddress, network, mapErr(err))
	}
	return nil
}

func (b *incusBackend) DeleteNetworkForward(ctx context.Context, network, listenAddress string) error {
	if err := b.project(ctx).DeleteNetworkForward(network, listenAddress); err != nil {
		return fmt.Errorf("delete forward %q on %q: %w", listenAddress, network, mapErr(err))
	}
	return nil
}
