package fake

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/adam/lxcon/internal/backend"
)

func (f *Fake) ListInstances(_ context.Context) ([]backend.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]backend.Instance, 0, len(f.instances))
	for _, in := range f.instances {
		out = append(out, f.view(in))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetInstance(_ context.Context, name string) (backend.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return backend.Instance{}, notFound(name)
	}
	return f.view(in), nil
}

func (f *Fake) CreateInstance(_ context.Context, opt backend.CreateOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[opt.Name]; ok {
		return conflict("instance %q already exists", opt.Name)
	}
	status := "Stopped"
	if opt.Start {
		status = "Running"
	}
	f.instances[opt.Name] = &instance{
		Instance: backend.Instance{
			Name:      opt.Name,
			Status:    status,
			Image:     opt.Image,
			CreatedAt: f.now(),
			Profiles:  []string{"default"},
		},
		config:  map[string]string{},
		devices: map[string]map[string]string{},
		files:   seedFiles(opt.Name),
	}
	f.logOp(fmt.Sprintf("Creating instance %q", opt.Name))
	return nil
}

func (f *Fake) StartInstance(_ context.Context, name string) error {
	return f.setStatus(name, "Running", "Starting")
}

func (f *Fake) StopInstance(_ context.Context, name string) error {
	return f.setStatus(name, "Stopped", "Stopping")
}

func (f *Fake) RestartInstance(_ context.Context, name string) error {
	return f.setStatus(name, "Running", "Restarting")
}

func (f *Fake) PauseInstance(_ context.Context, name string) error {
	return f.setStatus(name, "Frozen", "Pausing")
}

func (f *Fake) ResumeInstance(_ context.Context, name string) error {
	return f.setStatus(name, "Running", "Resuming")
}

func (f *Fake) setStatus(name, status, verb string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	in.Status = status
	f.logOp(fmt.Sprintf("%s instance %q", verb, name))
	return nil
}

func (f *Fake) DeleteInstance(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[name]; !ok {
		return notFound(name)
	}
	delete(f.instances, name)
	f.logOp(fmt.Sprintf("Deleting instance %q", name))
	return nil
}

func (f *Fake) CloneInstance(_ context.Context, src, dst string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	from, ok := f.instances[src]
	if !ok {
		return notFound(src)
	}
	if _, ok := f.instances[dst]; ok {
		return conflict("instance %q already exists", dst)
	}
	f.instances[dst] = &instance{
		Instance: backend.Instance{
			Name:      dst,
			Status:    "Stopped",
			Image:     from.Image,
			CreatedAt: f.now(),
		},
		files: maps.Clone(from.files),
	}
	f.logOp(fmt.Sprintf("Cloning instance %q to %q", src, dst))
	return nil
}
