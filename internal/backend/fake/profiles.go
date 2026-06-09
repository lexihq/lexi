package fake

import (
	"context"
	"sort"

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
	return f.profileView(name), nil
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
