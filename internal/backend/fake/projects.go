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
	p.Version = backend.Version(strconv.Itoa(f.remote(ctx).projectVersions[name]))
	return p, nil
}

// GetProjectUsage derives the usage rows the daemon's project state API
// reports: resource counts from the project's space, cpu/memory usage
// aggregated from instance limits, and limits parsed from the project's
// limits.* config keys (-1 when unset).
func (f *Fake) GetProjectUsage(ctx context.Context, name string) ([]backend.ProjectUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	rs := f.remote(ctx)
	p, ok := rs.projects[name]
	if !ok {
		return nil, notFoundf("project %q", name)
	}
	usage := map[string]int64{"instances": 0, "networks": 0, "cpu": 0, "memory": 0, "disk": 0}
	if sp, ok := rs.spaces[name]; ok {
		usage["instances"] = int64(len(sp.instances))
		for _, n := range sp.networks {
			if n.Managed { // limits.networks governs managed networks only
				usage["networks"]++
			}
		}
		for _, inst := range sp.instances {
			if cpu, err := strconv.ParseInt(inst.LimitsCPU, 10, 64); err == nil {
				usage["cpu"] += cpu
			}
			if mem, ok := parseSizeBytes(inst.LimitsMemory); ok {
				usage["memory"] += mem
			}
		}
	}
	out := make([]backend.ProjectUsage, 0, len(usage))
	for resource, used := range usage {
		limit := int64(-1)
		if raw, ok := p.Config["limits."+resource]; ok {
			switch resource {
			case "memory", "disk":
				if parsed, ok := parseSizeBytes(raw); ok {
					limit = parsed
				}
			default:
				if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
					limit = parsed
				}
			}
		}
		out = append(out, backend.ProjectUsage{Resource: resource, Usage: used, Limit: limit})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource < out[j].Resource })
	return out, nil
}

// parseSizeBytes parses the daemon's byte-size notation: a bare integer is
// bytes, decimal suffixes are 1000-based, binary suffixes 1024-based.
func parseSizeBytes(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	units := []struct {
		suffix string
		factor int64
	}{
		{"EiB", 1 << 60}, {"PiB", 1 << 50}, {"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
		{"EB", 1e18}, {"PB", 1e15}, {"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"B", 1},
	}
	factor := int64(1)
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			factor = u.factor
			s = strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n * factor, true
}

func (f *Fake) CreateProject(ctx context.Context, project backend.Project) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !validProjectName(project.Name) {
		return invalid("invalid project name %q", project.Name)
	}
	if _, ok := f.remote(ctx).projects[project.Name]; ok {
		return conflict("project %q already exists", project.Name)
	}
	// Daemon parity: omitted default-enabled features are injected as "true" at
	// create (images/profiles/storage.volumes). Networks and buckets are left
	// absent, so they share the default project's namespace.
	cfg := maps.Clone(project.Config)
	if cfg == nil {
		cfg = map[string]string{}
	}
	for _, feature := range []string{"features.images", "features.profiles", "features.storage.volumes"} {
		if _, ok := cfg[feature]; !ok {
			cfg[feature] = "true"
		}
	}
	f.remote(ctx).projects[project.Name] = backend.Project{Name: project.Name, Description: project.Description, Config: cfg}
	// A project owning its profiles starts with an empty default profile,
	// like the daemon (no root disk — instances need one configured).
	sp := f.remote(ctx).spaceFor(project.Name)
	if cfg["features.profiles"] == "true" {
		sp.profiles["default"] = backend.Profile{
			Name: "default", Description: "Default Incus profile",
			Config:  map[string]string{},
			Devices: map[string]map[string]string{},
		}
	}
	return nil
}

func (f *Fake) UpdateProject(ctx context.Context, name, description string, config map[string]string, version backend.Version) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.remote(ctx).projects[name]
	if !ok {
		return notFoundf("project %q", name)
	}
	// Empty version = unconditional, mirroring the Incus client's If-Match
	// semantics; a stale version means a concurrent writer landed first.
	if version != "" && string(version) != strconv.Itoa(f.remote(ctx).projectVersions[name]) {
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
