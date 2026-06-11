package incus

import (
	"context"
	"fmt"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

func (b *incusBackend) ListProjects(_ context.Context) ([]backend.Project, error) {
	ps, err := b.srv.GetProjects()
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", mapErr(err))
	}
	out := make([]backend.Project, 0, len(ps))
	for i := range ps {
		out = append(out, toProject(&ps[i], ""))
	}
	return out, nil
}

func (b *incusBackend) GetProject(_ context.Context, name string) (backend.Project, error) {
	p, etag, err := b.srv.GetProject(name)
	if err != nil {
		return backend.Project{}, fmt.Errorf("get project %q: %w", name, mapErr(err))
	}
	return toProject(p, etag), nil
}

func (b *incusBackend) CreateProject(_ context.Context, name, description string, config map[string]string) error {
	req := api.ProjectsPost{Name: name}
	req.Description = description
	req.Config = config
	if err := b.srv.CreateProject(req); err != nil {
		return fmt.Errorf("create project %q: %w", name, mapErr(err))
	}
	return nil
}

// UpdateProject replaces the project's description and config under the
// caller's version token (the GetProject etag); ProjectPut has no other
// fields, so there is nothing to GET-preserve.
func (b *incusBackend) UpdateProject(_ context.Context, name, description string, config map[string]string, version string) error {
	put := api.ProjectPut{Description: description, Config: config}
	if err := b.srv.UpdateProject(name, put, version); err != nil {
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
	op, err := b.srv.RenameProject(name, api.ProjectPost{Name: newName})
	if err := waitOp(ctx, op, err, "rename project %q", name); err != nil {
		if strings.Contains(err.Error(), "Only empty projects can be renamed") {
			return fmt.Errorf("%w: %w", backend.ErrConflict, err)
		}
		return err
	}
	return nil
}

func (b *incusBackend) DeleteProject(_ context.Context, name string) error {
	if name == "default" {
		return fmt.Errorf("the default project cannot be deleted: %w", backend.ErrInvalid)
	}
	if err := b.srv.DeleteProject(name); err != nil {
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
