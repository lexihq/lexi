package incus

import (
	"context"
	"fmt"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

func (b *incusBackend) ListProfiles(_ context.Context) ([]backend.Profile, error) {
	ps, err := b.srv.GetProfiles()
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", mapErr(err))
	}
	out := make([]backend.Profile, 0, len(ps))
	for i := range ps {
		out = append(out, toProfile(&ps[i]))
	}
	return out, nil
}

func (b *incusBackend) GetProfile(_ context.Context, name string) (backend.Profile, error) {
	p, _, err := b.srv.GetProfile(name)
	if err != nil {
		return backend.Profile{}, fmt.Errorf("get profile %q: %w", name, mapErr(err))
	}
	return toProfile(p), nil
}

// SetInstanceProfiles replaces the instance's ordered profile list (GET-then-PUT,
// matching UpdateLimits).
func (b *incusBackend) SetInstanceProfiles(ctx context.Context, name string, profiles []string) error {
	return b.mutateInstance(ctx, name, func(put *api.InstancePut) {
		put.Profiles = profiles
	}, "set profiles on %q", name)
}

func toProfile(p *api.Profile) backend.Profile {
	return backend.Profile{
		Name:        p.Name,
		Description: p.Description,
		Config:      p.Config,
		Devices:     p.Devices,
		UsedBy:      p.UsedBy,
	}
}
