package fake

import (
	"context"
	"maps"
	"sort"
	"strconv"

	"github.com/adam/lxcon/internal/backend"
)

func (f *Fake) ListProjects(_ context.Context) ([]backend.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]backend.Project, 0, len(f.projects))
	for name := range f.projects {
		out = append(out, f.projectView(name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetProject(_ context.Context, name string) (backend.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.projects[name]; !ok {
		return backend.Project{}, notFoundf("project %q", name)
	}
	p := f.projectView(name)
	p.Version = strconv.Itoa(f.projectVersions[name])
	return p, nil
}

func (f *Fake) CreateProject(_ context.Context, name, description string, config map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !validAPIName(name) {
		return invalid("invalid project name %q", name)
	}
	if _, ok := f.projects[name]; ok {
		return conflict("project %q already exists", name)
	}
	f.projects[name] = backend.Project{Name: name, Description: description, Config: maps.Clone(config)}
	return nil
}

func (f *Fake) UpdateProject(_ context.Context, name, description string, config map[string]string, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.projects[name]
	if !ok {
		return notFoundf("project %q", name)
	}
	// Empty version = unconditional, mirroring the Incus client's If-Match
	// semantics; a stale version means a concurrent writer landed first.
	if version != "" && version != strconv.Itoa(f.projectVersions[name]) {
		return conflict("project %q version %s", name, version)
	}
	p.Description = description
	p.Config = maps.Clone(config)
	f.projects[name] = p
	f.projectVersions[name]++
	return nil
}

func (f *Fake) RenameProject(_ context.Context, name, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.projects[name]
	if !ok {
		return notFoundf("project %q", name)
	}
	// Incus parity: the default project cannot be renamed.
	if name == "default" {
		return invalid("the default project cannot be renamed")
	}
	if !validAPIName(newName) {
		return invalid("invalid project name %q", newName)
	}
	if _, exists := f.projects[newName]; exists {
		return conflict("project %q already exists", newName)
	}
	p.Name = newName
	f.projects[newName] = p
	f.projectVersions[newName] = f.projectVersions[name] + 1
	delete(f.projects, name)
	delete(f.projectVersions, name)
	return nil
}

func (f *Fake) DeleteProject(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.projects[name]; !ok {
		return notFoundf("project %q", name)
	}
	// Incus parity: the default project cannot be deleted, and non-empty
	// projects refuse deletion.
	if name == "default" {
		return invalid("the default project cannot be deleted")
	}
	if used := f.projectUsedBy(name); len(used) > 0 {
		return conflict("project %q is not empty", name)
	}
	delete(f.projects, name)
	delete(f.projectVersions, name)
	return nil
}

// projectView materializes a project with a fresh UsedBy. Callers must hold
// the mutex.
func (f *Fake) projectView(name string) backend.Project {
	p := f.projects[name]
	p.Config = maps.Clone(p.Config)
	p.UsedBy = f.projectUsedBy(name)
	return p
}

// projectUsedBy lists API paths of resources living in the project. Until
// per-project scoping lands, all fake resources implicitly live in the
// default project. Callers must hold the mutex.
func (f *Fake) projectUsedBy(name string) []string {
	if name != "default" {
		return nil
	}
	var used []string
	for instName := range f.instances {
		used = append(used, "/1.0/instances/"+instName)
	}
	for profName := range f.profiles {
		used = append(used, "/1.0/profiles/"+profName)
	}
	for fp := range f.images {
		used = append(used, "/1.0/images/"+fp)
	}
	sort.Strings(used)
	return used
}
