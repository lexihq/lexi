package incus

import (
	"context"
	"fmt"
	"strings"

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
	p, etag, err := b.srv.GetProfile(name)
	if err != nil {
		return backend.Profile{}, fmt.Errorf("get profile %q: %w", name, mapErr(err))
	}
	out := toProfile(p)
	out.Version = etag
	return out, nil
}

func (b *incusBackend) CreateProfile(_ context.Context, name, description string) error {
	post := api.ProfilesPost{Name: name}
	post.Description = description
	if err := b.srv.CreateProfile(post); err != nil {
		return fmt.Errorf("create profile %q: %w", name, mapErr(err))
	}
	return nil
}

// UpdateProfile updates description and replaces the config map via
// GET-preserve-PUT: the PUT starts from the profile's current writable state,
// so its devices are never dropped. The version is the etag from GetProfile;
// the daemon rejects the PUT with 412 (mapped to ErrConflict) when the profile
// changed since that read. An empty version updates unconditionally.
func (b *incusBackend) UpdateProfile(_ context.Context, name, description string, config map[string]string, version string) error {
	p, _, err := b.srv.GetProfile(name)
	if err != nil {
		return fmt.Errorf("get profile %q: %w", name, mapErr(err))
	}
	put := p.Writable()
	put.Description = description
	put.Config = config
	if err := b.srv.UpdateProfile(name, put, version); err != nil {
		return fmt.Errorf("update profile %q: %w", name, mapErr(err))
	}
	return nil
}

// DeleteProfile refuses "default" and in-use profiles up front: the daemon's
// in-use failure is a plain error mapErr cannot type, so the pre-check is what
// produces a stable ErrConflict for the HTTP layer.
func (b *incusBackend) DeleteProfile(_ context.Context, name string) error {
	if name == "default" {
		return fmt.Errorf("the default profile cannot be deleted: %w", backend.ErrInvalid)
	}
	p, _, err := b.srv.GetProfile(name)
	if err != nil {
		return fmt.Errorf("get profile %q: %w", name, mapErr(err))
	}
	if n := len(p.UsedBy); n > 0 {
		return fmt.Errorf("profile %q is in use by %d instance(s): %w", name, n, backend.ErrConflict)
	}
	if err := b.srv.DeleteProfile(name); err != nil {
		// An attach racing the UsedBy pre-check surfaces here as the daemon's
		// untyped "in use" error; map it to the same conflict the pre-check gives.
		if strings.Contains(err.Error(), "in use") {
			return fmt.Errorf("profile %q is in use: %w", name, backend.ErrConflict)
		}
		return fmt.Errorf("delete profile %q: %w", name, mapErr(err))
	}
	return nil
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
