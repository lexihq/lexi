package fake

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adam/lxcon/internal/backend"
)

func (f *Fake) ListProjects(ctx context.Context) ([]backend.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]backend.Project, 0, len(f.remote(ctx).projects))
	for name := range f.remote(ctx).projects {
		out = append(out, f.projectView(f.remote(ctx), name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetProject(ctx context.Context, name string) (backend.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.remote(ctx).projects[name]; !ok {
		return backend.Project{}, notFoundf("project %q", name)
	}
	p := f.projectView(f.remote(ctx), name)
	p.Version = strconv.Itoa(f.remote(ctx).projectVersions[name])
	return p, nil
}

func (f *Fake) CreateProject(ctx context.Context, name, description string, config map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !validProjectName(name) {
		return invalid("invalid project name %q", name)
	}
	if _, ok := f.remote(ctx).projects[name]; ok {
		return conflict("project %q already exists", name)
	}
	// Daemon parity: omitted default-enabled features are injected as "true"
	// at create (images/profiles/storage.volumes; buckets exist daemon-side
	// but lxcon doesn't model them). Networks stay absent = shared.
	cfg := maps.Clone(config)
	if cfg == nil {
		cfg = map[string]string{}
	}
	for _, feature := range []string{"features.images", "features.profiles", "features.storage.volumes"} {
		if _, ok := cfg[feature]; !ok {
			cfg[feature] = "true"
		}
	}
	f.remote(ctx).projects[name] = backend.Project{Name: name, Description: description, Config: cfg}
	// A project owning its profiles starts with an empty default profile,
	// like the daemon (no root disk — instances need one configured).
	sp := f.remote(ctx).spaceFor(name)
	if cfg["features.profiles"] == "true" {
		sp.profiles["default"] = backend.Profile{
			Name: "default", Description: "Default Incus profile",
			Config:  map[string]string{},
			Devices: map[string]map[string]string{},
		}
	}
	return nil
}

func (f *Fake) UpdateProject(ctx context.Context, name, description string, config map[string]string, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.remote(ctx).projects[name]
	if !ok {
		return notFoundf("project %q", name)
	}
	// Empty version = unconditional, mirroring the Incus client's If-Match
	// semantics; a stale version means a concurrent writer landed first.
	if version != "" && version != strconv.Itoa(f.remote(ctx).projectVersions[name]) {
		return conflict("project %q version %s", name, version)
	}
	// Incus parity: features cannot change once the project holds resources
	// (flipping one would re-route existing resources to another namespace).
	if f.projectFeatureChanged(p.Config, config) {
		used := slices.DeleteFunc(f.projectUsedBy(f.remote(ctx), name), isSeededDefaultProfile)
		if len(used) > 0 {
			return invalid("features can only be changed on empty projects")
		}
	}
	p.Description = description
	p.Config = maps.Clone(config)
	f.remote(ctx).projects[name] = p
	f.remote(ctx).projectVersions[name]++
	return nil
}

func (f *Fake) RenameProject(ctx context.Context, name, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.remote(ctx).projects[name]
	if !ok {
		return notFoundf("project %q", name)
	}
	// Incus parity: the default project cannot be renamed.
	if name == "default" {
		return invalid("the default project cannot be renamed")
	}
	if !validProjectName(newName) {
		return invalid("invalid project name %q", newName)
	}
	if _, exists := f.remote(ctx).projects[newName]; exists {
		return conflict("project %q already exists", newName)
	}
	p.Name = newName
	f.remote(ctx).projects[newName] = p
	f.remote(ctx).projectVersions[newName] = f.remote(ctx).projectVersions[name] + 1
	delete(f.remote(ctx).projects, name)
	delete(f.remote(ctx).projectVersions, name)
	// The space and per-pool volume namespaces follow the rename.
	if sp, ok := f.remote(ctx).spaces[name]; ok {
		f.remote(ctx).spaces[newName] = sp
		delete(f.remote(ctx).spaces, name)
	}
	for _, pool := range f.remote(ctx).pools {
		if vols, ok := pool.volumes[name]; ok {
			pool.volumes[newName] = vols
			delete(pool.volumes, name)
		}
	}
	return nil
}

func (f *Fake) DeleteProject(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.remote(ctx).projects[name]; !ok {
		return notFoundf("project %q", name)
	}
	// Incus parity: the default project cannot be deleted, and non-empty
	// projects refuse deletion.
	if name == "default" {
		return invalid("the default project cannot be deleted")
	}
	// The project's own (seeded) default profile does not count against
	// emptiness, matching the daemon's projectIsEmpty.
	used := slices.DeleteFunc(f.projectUsedBy(f.remote(ctx), name), isSeededDefaultProfile)
	if len(used) > 0 {
		return conflict("project %q is not empty", name)
	}
	delete(f.remote(ctx).projects, name)
	delete(f.remote(ctx).projectVersions, name)
	delete(f.remote(ctx).spaces, name)
	for _, pool := range f.remote(ctx).pools {
		delete(pool.volumes, name)
	}
	return nil
}

// isSeededDefaultProfile matches a project's own default profile in UsedBy
// regardless of the daemon's ?project= qualification — it never counts
// against emptiness.
func isSeededDefaultProfile(u string) bool {
	path, _, _ := strings.Cut(u, "?")
	return path == "/1.0/profiles/default"
}

// projectFeatureChanged reports whether any features.* key differs between
// the configs (missing keys read as "false", matching the daemon's effective
// view). Callers must hold the mutex.
func (f *Fake) projectFeatureChanged(oldCfg, newCfg map[string]string) bool {
	keys := map[string]bool{}
	for k := range oldCfg {
		if strings.HasPrefix(k, "features.") {
			keys[k] = true
		}
	}
	for k := range newCfg {
		if strings.HasPrefix(k, "features.") {
			keys[k] = true
		}
	}
	for k := range keys {
		oldOn, newOn := oldCfg[k] == "true", newCfg[k] == "true"
		if oldOn == newOn {
			continue
		}
		// The daemon allows enabling features.networks.zones on non-empty
		// projects (CanEnableNonEmpty); every other transition is frozen.
		if k == "features.networks.zones" && !oldOn {
			continue
		}
		return true
	}
	return false
}

// validProjectName mirrors the daemon's full IsAPIName for projects: the
// shared validAPIName rules plus alphanumeric start/end.
func validProjectName(name string) bool {
	return validAPIName(name) && apiNameEnds.MatchString(name)
}

// projectView materializes a project with a fresh UsedBy. Callers must hold
// the mutex.
func (f *Fake) projectView(rs *remoteState, name string) backend.Project {
	p := rs.projects[name]
	p.Config = maps.Clone(p.Config)
	p.UsedBy = f.projectUsedBy(rs, name)
	return p
}

// projectUsedBy lists API paths of resources living in the project's space
// (networks and ACLs only when the project owns them via features.networks).
// Callers must hold the mutex.
func (f *Fake) projectUsedBy(rs *remoteState, name string) []string {
	sp, ok := rs.spaces[name]
	if !ok {
		return nil
	}
	// Daemon parity: UsedBy entries of non-default projects carry the
	// project query suffix (api.URL.Project).
	suffix := ""
	if name != "default" {
		suffix = "?project=" + name
	}
	var used []string
	for instName := range sp.instances {
		used = append(used, "/1.0/instances/"+instName+suffix)
	}
	for profName := range sp.profiles {
		used = append(used, "/1.0/profiles/"+profName+suffix)
	}
	for fp := range sp.images {
		used = append(used, "/1.0/images/"+fp+suffix)
	}
	if name == "default" || rs.projects[name].Config["features.networks"] == "true" {
		for netName := range sp.networks {
			used = append(used, "/1.0/networks/"+netName+suffix)
		}
		for aclName := range sp.acls {
			used = append(used, "/1.0/network-acls/"+aclName+suffix)
		}
	}
	for poolName, pool := range rs.pools {
		for volName := range pool.volumes[name] {
			used = append(used, "/1.0/storage-pools/"+poolName+"/volumes/custom/"+volName+suffix)
		}
	}
	sort.Strings(used)
	return used
}
