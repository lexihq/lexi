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
	n, etag, err := b.srv.GetNetwork(name)
	if err != nil {
		return backend.Network{}, fmt.Errorf("get network %q: %w", name, mapErr(err))
	}
	out := toNetwork(n)
	out.Version = etag
	return out, nil
}

// UpdateNetwork updates description and replaces the config map via
// GET-preserve-PUT: the PUT starts from the network's current writable state
// so fields beyond description/config are never dropped. The version is the
// etag from GetNetwork; the daemon rejects the PUT with 412 (mapped to
// ErrConflict) when the network changed since that read. An empty version
// sends no If-Match and updates unconditionally.
func (b *incusBackend) UpdateNetwork(_ context.Context, name, description string, config map[string]string, version string) error {
	n, _, err := b.srv.GetNetwork(name)
	if err != nil {
		return fmt.Errorf("get network %q: %w", name, mapErr(err))
	}
	if !n.Managed {
		return fmt.Errorf("network %q is unmanaged: %w", name, backend.ErrInvalid)
	}
	put := n.Writable()
	put.Description = description
	put.Config = config
	if err := b.srv.UpdateNetwork(name, put, version); err != nil {
		return fmt.Errorf("update network %q: %w", name, mapErr(err))
	}
	return nil
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
