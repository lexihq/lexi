package incus

import (
	"context"
	"fmt"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

func (b *incusBackend) ListNetworks(_ context.Context) ([]backend.Network, error) {
	ns, err := b.srv.GetNetworks()
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", mapErr(err))
	}
	out := make([]backend.Network, 0, len(ns))
	for i := range ns {
		out = append(out, toNetwork(&ns[i]))
	}
	return out, nil
}

func (b *incusBackend) GetNetwork(_ context.Context, name string) (backend.Network, error) {
	n, _, err := b.srv.GetNetwork(name)
	if err != nil {
		return backend.Network{}, fmt.Errorf("get network %q: %w", name, mapErr(err))
	}
	return toNetwork(n), nil
}

func (b *incusBackend) CreateNetwork(_ context.Context, n backend.Network) error {
	post := api.NetworksPost{Name: n.Name, Type: n.Type}
	post.Description = n.Description
	post.Config = n.Config
	if err := b.srv.CreateNetwork(post); err != nil {
		return fmt.Errorf("create network %q: %w", n.Name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) DeleteNetwork(_ context.Context, name string) error {
	if err := b.srv.DeleteNetwork(name); err != nil {
		return fmt.Errorf("delete network %q: %w", name, mapErr(err))
	}
	return nil
}

func toNetwork(n *api.Network) backend.Network {
	return backend.Network{
		Name:        n.Name,
		Type:        n.Type,
		Managed:     n.Managed,
		Description: n.Description,
		Config:      n.Config,
		UsedBy:      n.UsedBy,
	}
}
