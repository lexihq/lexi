package fake

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
)

func (f *Fake) ListProfiles(ctx context.Context) ([]backend.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.profiles")

	out := make([]backend.Profile, 0, len(sp.profiles))
	for name := range sp.profiles {
		out = append(out, f.profileView(ctx, sp, name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetProfile(ctx context.Context, name string) (backend.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.profiles")

	if _, ok := sp.profiles[name]; !ok {
		return backend.Profile{}, notFoundf("profile %q", name)
	}
	p := f.profileView(ctx, sp, name)
	p.Version = strconv.Itoa(sp.profileVersions[name])
	return p, nil
}

func (f *Fake) CreateProfile(ctx context.Context, name, description string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.profiles")

	// Incus parity: API object names exclude whitespace and path separators.
	if strings.ContainsAny(name, " \t\n/") {
		return invalid("invalid profile name %q", name)
	}
	if _, ok := sp.profiles[name]; ok {
		return conflict("profile %q already exists", name)
	}
	sp.profiles[name] = backend.Profile{
		Name: name, Description: description,
		Config:  map[string]string{},
		Devices: map[string]map[string]string{},
	}
	return nil
}

func (f *Fake) UpdateProfile(ctx context.Context, name, description string, config map[string]string, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.profiles")

	p, ok := sp.profiles[name]
	if !ok {
		return notFoundf("profile %q", name)
	}
	// Empty version = unconditional, mirroring UpdateServerConfig; a stale
	// version means a concurrent writer landed first.
	if version != "" && version != strconv.Itoa(sp.profileVersions[name]) {
		return conflict("profile %q version %s", name, version)
	}
	p.Description = description
	p.Config = maps.Clone(config)
	if p.Config == nil {
		p.Config = map[string]string{}
	}
	sp.profiles[name] = p // devices untouched
	sp.profileVersions[name]++
	return nil
}

func (f *Fake) DeleteProfile(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.profiles")

	if _, ok := sp.profiles[name]; !ok {
		return notFoundf("profile %q", name)
	}
	if name == "default" {
		return invalid("the default profile cannot be deleted")
	}
	for instName, in := range f.profileUsers(ctx) {
		if slices.Contains(in.Profiles, name) {
			return conflict("profile %q is in use by %q", name, instName)
		}
	}
	delete(sp.profiles, name)
	delete(sp.profileVersions, name)
	return nil
}

func (f *Fake) RenameProfile(ctx context.Context, name, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.profiles")

	p, ok := sp.profiles[name]
	if !ok {
		return notFoundf("profile %q", name)
	}
	if name == "default" {
		return invalid("the default profile cannot be renamed")
	}
	if !validAPIName(newName) {
		return invalid("invalid profile name %q", newName)
	}
	if _, exists := sp.profiles[newName]; exists {
		return conflict("profile %q already exists", newName)
	}
	p.Name = newName
	sp.profiles[newName] = p
	sp.profileVersions[newName] = sp.profileVersions[name] + 1
	delete(sp.profiles, name)
	delete(sp.profileVersions, name)
	// Assigned instances follow the rename, as the daemon's DB-level rename
	// does (instances reference profiles by ID there) — across every project
	// sharing this profile namespace.
	for _, in := range f.profileUsers(ctx) {
		for i, pn := range in.Profiles {
			if pn == name {
				in.Profiles[i] = newName
			}
		}
	}
	return nil
}

// profileUsers collects the instances of every project whose effective
// profile namespace is the request's, keyed by instance name — the daemon
// tracks profile usage across all projects sharing the owner. Callers must
// hold the mutex.
func (f *Fake) profileUsers(ctx context.Context) map[string]*instance {
	owner := f.featureProject(ctx, "features.profiles")
	out := map[string]*instance{}
	for project, spc := range f.remote(ctx).spaces {
		if f.remote(ctx).featureProjectName(project, "features.profiles") != owner {
			continue
		}
		maps.Copy(out, spc.instances)
	}
	return out
}

func (f *Fake) AddProfileDevice(ctx context.Context, profile, device string, config map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.profiles")

	p, ok := sp.profiles[profile]
	if !ok {
		return notFoundf("profile %q", profile)
	}
	if p.Devices == nil {
		p.Devices = map[string]map[string]string{}
	}
	p.Devices[device] = maps.Clone(config)
	sp.profiles[profile] = p
	sp.profileVersions[profile]++
	return nil
}

func (f *Fake) UpdateProfileDevice(ctx context.Context, profile, device string, config map[string]string, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.profiles")

	p, ok := sp.profiles[profile]
	if !ok {
		return notFoundf("profile %q", profile)
	}
	if _, ok := p.Devices[device]; !ok {
		return notFoundf("device %q on profile %q", device, profile)
	}
	if version != "" && version != strconv.Itoa(sp.profileVersions[profile]) {
		return conflict("profile %q version %s", profile, version)
	}
	p.Devices[device] = maps.Clone(config)
	sp.profiles[profile] = p
	sp.profileVersions[profile]++
	return nil
}

func (f *Fake) RemoveProfileDevice(ctx context.Context, profile, device string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.profiles")

	p, ok := sp.profiles[profile]
	if !ok {
		return notFoundf("profile %q", profile)
	}
	if _, ok := p.Devices[device]; !ok {
		return notFoundf("device %q on profile %q", device, profile)
	}
	delete(p.Devices, device)
	sp.profiles[profile] = p
	sp.profileVersions[profile]++
	return nil
}

func (f *Fake) SetInstanceProfiles(ctx context.Context, name string, profiles []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.featureSpace(ctx, "features.profiles")

	in, ok := f.space(ctx).instances[name]
	if !ok {
		return notFound(name)
	}
	for _, p := range profiles {
		if _, ok := sp.profiles[p]; !ok {
			return invalid("unknown profile %q", p)
		}
	}
	in.Profiles = append([]string(nil), profiles...)
	return nil
}

// profileView materializes a profile with a fresh UsedBy from the instances
// of every project sharing the profile namespace. Callers must hold the mutex.
func (f *Fake) profileView(ctx context.Context, sp *space, name string) backend.Profile {
	p := sp.profiles[name]
	// Clone the maps: the struct copy above still shares them with the store,
	// and callers read the result after the mutex is released while writers
	// (Add/Update/RemoveProfileDevice) mutate the stored maps in place.
	p.Config = maps.Clone(p.Config)
	p.Devices = cloneDevices(p.Devices)
	var usedBy []string
	for instName, in := range f.profileUsers(ctx) {
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
