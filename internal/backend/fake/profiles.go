package fake

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strconv"

	"github.com/adam/lxcon/internal/backend"
)

func (f *Fake) ListProfiles(_ context.Context) ([]backend.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]backend.Profile, 0, len(f.profiles))
	for name := range f.profiles {
		out = append(out, f.profileView(name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetProfile(_ context.Context, name string) (backend.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.profiles[name]; !ok {
		return backend.Profile{}, notFoundf("profile %q", name)
	}
	p := f.profileView(name)
	p.Version = strconv.Itoa(f.profileVersions[name])
	return p, nil
}

func (f *Fake) CreateProfile(_ context.Context, name, description string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.profiles[name]; ok {
		return conflict("profile %q already exists", name)
	}
	f.profiles[name] = backend.Profile{
		Name: name, Description: description,
		Config:  map[string]string{},
		Devices: map[string]map[string]string{},
	}
	return nil
}

func (f *Fake) UpdateProfile(_ context.Context, name, description string, config map[string]string, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.profiles[name]
	if !ok {
		return notFoundf("profile %q", name)
	}
	// Empty version = unconditional, mirroring UpdateServerConfig; a stale
	// version means a concurrent writer landed first.
	if version != "" && version != strconv.Itoa(f.profileVersions[name]) {
		return conflict("profile %q version %s", name, version)
	}
	p.Description = description
	p.Config = maps.Clone(config)
	if p.Config == nil {
		p.Config = map[string]string{}
	}
	f.profiles[name] = p // devices untouched
	f.profileVersions[name]++
	return nil
}

func (f *Fake) DeleteProfile(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.profiles[name]; !ok {
		return notFoundf("profile %q", name)
	}
	if name == "default" {
		return invalid("the default profile cannot be deleted")
	}
	for instName, in := range f.instances {
		if slices.Contains(in.Profiles, name) {
			return conflict("profile %q is in use by %q", name, instName)
		}
	}
	delete(f.profiles, name)
	delete(f.profileVersions, name)
	return nil
}

func (f *Fake) SetInstanceProfiles(_ context.Context, name string, profiles []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	for _, p := range profiles {
		if _, ok := f.profiles[p]; !ok {
			return invalid("unknown profile %q", p)
		}
	}
	in.Profiles = append([]string(nil), profiles...)
	return nil
}

// profileView materializes a profile with a fresh UsedBy from current instances.
// Callers must hold the mutex.
func (f *Fake) profileView(name string) backend.Profile {
	p := f.profiles[name]
	var usedBy []string
	for instName, in := range f.instances {
		for _, pn := range in.Profiles {
			if pn == name {
				usedBy = append(usedBy, instName)
			}
		}
	}
	sort.Strings(usedBy)
	p.UsedBy = usedBy
	return p
}
