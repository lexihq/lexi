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
	inst, etag, err := b.srv.GetInstance(name)
	if err != nil {
		return fmt.Errorf("get instance %q: %w", name, mapErr(err))
	}
	put := inst.Writable()
	put.Profiles = profiles
	op, err := b.srv.UpdateInstance(name, put, etag)
	if err != nil {
		return fmt.Errorf("set profiles on %q: %w", name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("set profiles on %q: %w", name, mapErr(err))
	}
	return nil
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
