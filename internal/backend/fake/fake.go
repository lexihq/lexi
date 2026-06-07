// Package fake is an in-memory backend.Backend used to drive fast, daemon-free
// unit tests of the HTTP and UI layers.
package fake

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/adam/lxcon/internal/backend"
)

// Compile-time proof that Fake satisfies the Backend contract.
var _ backend.Backend = (*Fake)(nil)

type instance struct {
	backend.Instance
	snapshots []backend.Snapshot
}

// Fake is a mutex-guarded, in-memory Backend with a deterministic clock.
type Fake struct {
	mu        sync.Mutex
	instances map[string]*instance
	clock     time.Time
}

// New returns an empty fake backend.
func New() *Fake {
	return &Fake{
		instances: make(map[string]*instance),
		clock:     time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
}

// now returns a deterministic, monotonically increasing timestamp.
// Callers must hold the mutex.
func (f *Fake) now() time.Time {
	f.clock = f.clock.Add(time.Second)
	return f.clock
}

func (f *Fake) Capabilities() backend.Capabilities {
	return backend.Capabilities{
		Tier:       backend.TierFake,
		ServerInfo: "fake backend",
		Snapshots:  true,
		Clone:      true,
	}
}

// view materializes the public Instance with an up-to-date snapshot count.
// Callers must hold the mutex.
func (f *Fake) view(in *instance) backend.Instance {
	out := in.Instance
	out.Snapshots = len(in.snapshots)
	out.IPv4 = append([]string(nil), in.IPv4...)
	return out
}

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
		return fmt.Errorf("instance %q already exists", opt.Name)
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
		},
	}
	return nil
}

func (f *Fake) StartInstance(_ context.Context, name string) error {
	return f.setStatus(name, "Running")
}

func (f *Fake) StopInstance(_ context.Context, name string) error {
	return f.setStatus(name, "Stopped")
}

func (f *Fake) setStatus(name, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	in.Status = status
	return nil
}

func (f *Fake) DeleteInstance(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[name]; !ok {
		return notFound(name)
	}
	delete(f.instances, name)
	return nil
}

func (f *Fake) ListSnapshots(_ context.Context, name string) ([]backend.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return nil, notFound(name)
	}
	return append([]backend.Snapshot(nil), in.snapshots...), nil
}

func (f *Fake) CreateSnapshot(_ context.Context, name, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	for _, s := range in.snapshots {
		if s.Name == snapshot {
			return fmt.Errorf("snapshot %q already exists on %q", snapshot, name)
		}
	}
	in.snapshots = append(in.snapshots, backend.Snapshot{Name: snapshot, CreatedAt: f.now()})
	return nil
}

func (f *Fake) RestoreSnapshot(_ context.Context, name, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	for _, s := range in.snapshots {
		if s.Name == snapshot {
			return nil
		}
	}
	return fmt.Errorf("snapshot %q not found on %q", snapshot, name)
}

func (f *Fake) DeleteSnapshot(_ context.Context, name, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	for i, s := range in.snapshots {
		if s.Name == snapshot {
			in.snapshots = append(in.snapshots[:i], in.snapshots[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("snapshot %q not found on %q", snapshot, name)
}

func (f *Fake) CloneInstance(_ context.Context, src, dst string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	from, ok := f.instances[src]
	if !ok {
		return notFound(src)
	}
	if _, ok := f.instances[dst]; ok {
		return fmt.Errorf("instance %q already exists", dst)
	}
	f.instances[dst] = &instance{
		Instance: backend.Instance{
			Name:      dst,
			Status:    "Stopped",
			Image:     from.Image,
			CreatedAt: f.now(),
		},
	}
	return nil
}

// curatedImages is the fake's stand-in for the v1 curated alias set the incus
// driver will expose (debian/12, ubuntu/24.04, alpine/edge). Arch is fixed here
// since the fake has no host to probe.
var curatedImages = []backend.Image{
	{Alias: "debian/12", Description: "Debian 12 (bookworm)", Arch: "arm64"},
	{Alias: "ubuntu/24.04", Description: "Ubuntu 24.04 LTS", Arch: "arm64"},
	{Alias: "alpine/edge", Description: "Alpine Edge", Arch: "arm64"},
}

func (f *Fake) ListImages(_ context.Context) ([]backend.Image, error) {
	return append([]backend.Image(nil), curatedImages...), nil
}

func notFound(name string) error {
	return fmt.Errorf("instance %q not found", name)
}
