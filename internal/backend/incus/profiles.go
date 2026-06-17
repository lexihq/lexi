package incus

import (
	"context"
	"fmt"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

func (b *incusBackend) ListProfiles(ctx context.Context) ([]backend.Profile, error) {
	ps, err := b.project(ctx).GetProfiles()
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", mapErr(err))
	}
	out := make([]backend.Profile, 0, len(ps))
	for i := range ps {
		out = append(out, toProfile(&ps[i]))
	}
	return out, nil
}

func (b *incusBackend) GetProfile(ctx context.Context, name string) (backend.Profile, error) {
	p, etag, err := b.project(ctx).GetProfile(name)
	if err != nil {
		return backend.Profile{}, fmt.Errorf("get profile %q: %w", name, mapErr(err))
	}
	out := toProfile(p)
	out.Version = etag
	return out, nil
}

func (b *incusBackend) CreateProfile(ctx context.Context, name, description string) error {
	post := api.ProfilesPost{Name: name}
	post.Description = description
	if err := b.project(ctx).CreateProfile(post); err != nil {
		return fmt.Errorf("create profile %q: %w", name, mapErr(err))
	}
	return nil
}

// UpdateProfile updates description and replaces the config map via
// GET-preserve-PUT: the PUT starts from the profile's current writable state,
// so its devices are never dropped. The version is the etag from GetProfile;
// the daemon rejects the PUT with 412 (mapped to ErrConflict) when the profile
// changed since that read. An empty version updates unconditionally.
func (b *incusBackend) UpdateProfile(ctx context.Context, name, description string, config map[string]string, version string) error {
	p, _, err := b.project(ctx).GetProfile(name)
	if err != nil {
		return fmt.Errorf("get profile %q: %w", name, mapErr(err))
	}
	put := p.Writable()
	put.Description = description
	put.Config = config
	if err := b.project(ctx).UpdateProfile(name, put, version); err != nil {
		return fmt.Errorf("update profile %q: %w", name, mapErr(err))
	}
	return nil
}

// DeleteProfile refuses "default" and in-use profiles up front: the daemon's
// in-use failure is a plain error mapErr cannot type, so the pre-check is what
// produces a stable ErrConflict for the HTTP layer.
func (b *incusBackend) DeleteProfile(ctx context.Context, name string) error {
	if name == "default" {
		return fmt.Errorf("the default profile cannot be deleted: %w", backend.ErrInvalid)
	}
	p, _, err := b.project(ctx).GetProfile(name)
	if err != nil {
		return fmt.Errorf("get profile %q: %w", name, mapErr(err))
	}
	if n := len(p.UsedBy); n > 0 {
		return fmt.Errorf("profile %q is in use by %d instance(s): %w", name, n, backend.ErrConflict)
	}
	if err := b.project(ctx).DeleteProfile(name); err != nil {
		// An attach racing the UsedBy pre-check surfaces here as the daemon's
		// untyped "in use" error; map it to the same conflict the pre-check gives.
		if strings.Contains(err.Error(), "in use") {
			return fmt.Errorf("profile %q is in use: %w", name, backend.ErrConflict)
		}
		return fmt.Errorf("delete profile %q: %w", name, mapErr(err))
	}
	return nil
}

// RenameProfile renames a profile. "default" is refused up front; the target
// name collision surfaces from the daemon as ErrConflict.
func (b *incusBackend) RenameProfile(ctx context.Context, name, newName string) error {
	if name == "default" {
		return fmt.Errorf("the default profile cannot be renamed: %w", backend.ErrInvalid)
	}
	if err := b.project(ctx).RenameProfile(name, api.ProfilePost{Name: newName}); err != nil {
		return fmt.Errorf("rename profile %q: %w", name, mapErr(err))
	}
	return nil
}

// AddProfileDevice attaches (or overwrites) a device via GET-preserve-PUT, so
// the profile's config and other devices are untouched.
func (b *incusBackend) AddProfileDevice(ctx context.Context, profile, device string, config map[string]string) error {
	return b.mutateProfile(ctx, profile, "", func(put *api.ProfilePut) error {
		if put.Devices == nil {
			put.Devices = map[string]map[string]string{}
		}
		put.Devices[device] = config
		return nil
	}, "add device %q to profile %q", device, profile)
}

// UpdateProfileDevice replaces an existing device's config map, conditionally on
// the profile version (etag).
func (b *incusBackend) UpdateProfileDevice(ctx context.Context, profile, device string, config map[string]string, version string) error {
	return b.mutateProfile(ctx, profile, version, func(put *api.ProfilePut) error {
		if _, ok := put.Devices[device]; !ok {
			return fmt.Errorf("device %q on profile %q: %w", device, profile, backend.ErrNotFound)
		}
		put.Devices[device] = config
		return nil
	}, "update device %q on profile %q", device, profile)
}

// RemoveProfileDevice detaches a device. Absent devices 404 before any PUT.
func (b *incusBackend) RemoveProfileDevice(ctx context.Context, profile, device string) error {
	return b.mutateProfile(ctx, profile, "", func(put *api.ProfilePut) error {
		if _, ok := put.Devices[device]; !ok {
			return fmt.Errorf("device %q on profile %q: %w", device, profile, backend.ErrNotFound)
		}
		delete(put.Devices, device)
		return nil
	}, "remove device %q from profile %q", device, profile)
}

// mutateProfile GETs the profile, applies mutate to its writable copy, and PUTs
// it back conditionally on version (empty version uses the fresh etag). mutate
// may return a sentinel error (e.g. ErrNotFound) to abort before the PUT.
func (b *incusBackend) mutateProfile(ctx context.Context, name, version string, mutate func(*api.ProfilePut) error, action string, args ...any) error {
	p, etag, err := b.project(ctx).GetProfile(name)
	if err != nil {
		return fmt.Errorf("get profile %q: %w", name, mapErr(err))
	}
	put := p.Writable()
	if err := mutate(&put); err != nil {
		return err
	}
	if version == "" {
		version = etag
	}
	if err := b.project(ctx).UpdateProfile(name, put, version); err != nil {
		return fmt.Errorf(action+": %w", append(args, mapErr(err))...)
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
