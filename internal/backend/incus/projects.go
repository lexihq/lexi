package incus

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

func (b *incusBackend) ListProjects(ctx context.Context) ([]backend.Project, error) {
	ps, err := b.server(ctx).GetProjects()
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", mapErr(err))
	}
	out := make([]backend.Project, 0, len(ps))
	for i := range ps {
		out = append(out, toProject(&ps[i], ""))
	}
	return out, nil
}

func (b *incusBackend) GetProject(ctx context.Context, name string) (backend.Project, error) {
	p, etag, err := b.server(ctx).GetProject(name)
	if err != nil {
		return backend.Project{}, fmt.Errorf("get project %q: %w", name, mapErr(err))
	}
	return toProject(p, etag), nil
}

// GetProjectUsage maps the project state API's resource map into sorted
// usage rows.
func (b *incusBackend) GetProjectUsage(ctx context.Context, name string) ([]backend.ProjectUsage, error) {
	st, err := b.server(ctx).GetProjectState(name)
	if err != nil {
		return nil, fmt.Errorf("get project %q state: %w", name, mapErr(err))
	}
	out := make([]backend.ProjectUsage, 0, len(st.Resources))
	for resource, r := range st.Resources {
		out = append(out, backend.ProjectUsage{Resource: resource, Usage: r.Usage, Limit: r.Limit})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource < out[j].Resource })
	return out, nil
}

func (b *incusBackend) CreateProject(ctx context.Context, name, description string, config map[string]string) error {
	req := api.ProjectsPost{Name: name}
	req.Description = description
	req.Config = config
	if err := b.server(ctx).CreateProject(req); err != nil {
		return fmt.Errorf("create project %q: %w", name, mapErr(err))
	}
	return nil
}

// UpdateProject replaces the project's description and config under the
// caller's version token (the GetProject etag); ProjectPut has no other
// fields, so there is nothing to GET-preserve.
func (b *incusBackend) UpdateProject(ctx context.Context, name, description string, config map[string]string, version string) error {
	put := api.ProjectPut{Description: description, Config: config}
	if err := b.server(ctx).UpdateProject(name, put, version); err != nil {
		return fmt.Errorf("update project %q: %w", name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) RenameProject(ctx context.Context, name, newName string) error {
	// Deterministic guard: the daemon refuses renaming the default project;
	// matching its message is more fragile than the pre-check.
	if name == "default" {
		return fmt.Errorf("the default project cannot be renamed: %w", backend.ErrInvalid)
	}
	// Rename failures arrive as plain operation errors with no HTTP status,
	// so the daemon's name validation can't be mapped after the fact.
	if !validAPIName(newName) || !apiNameEnds.MatchString(newName) {
		return fmt.Errorf("invalid project name %q: %w", newName, backend.ErrInvalid)
	}
	op, err := b.server(ctx).RenameProject(name, api.ProjectPost{Name: newName})
	if err := waitOp(ctx, op, err, "rename project %q", name); err != nil {
		if strings.Contains(err.Error(), "Only empty projects can be renamed") {
			return fmt.Errorf("%w: %w", backend.ErrConflict, err)
		}
		return err
	}
	return nil
}

func (b *incusBackend) DeleteProject(ctx context.Context, name string) error {
	if name == "default" {
		return fmt.Errorf("the default project cannot be deleted: %w", backend.ErrInvalid)
	}
	if err := b.server(ctx).DeleteProject(name); err != nil {
		// The daemon refuses non-empty projects with a plain 500 ("Only
		// empty projects can be removed."), which mapErr cannot classify.
		if strings.Contains(err.Error(), "Only empty projects") {
			return fmt.Errorf("delete project %q: %w: %w", name, backend.ErrConflict, err)
		}
		return fmt.Errorf("delete project %q: %w", name, mapErr(err))
	}
	return nil
}

func toProject(p *api.Project, etag string) backend.Project {
	return backend.Project{
		Name:        p.Name,
		Description: p.Description,
		Config:      p.Config,
		UsedBy:      p.UsedBy,
		Version:     etag,
	}
}
