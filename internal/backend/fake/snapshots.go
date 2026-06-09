package fake

import (
	"context"

	"github.com/adam/lxcon/internal/backend"
)

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
			return conflict("snapshot %q already exists on %q", snapshot, name)
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
	return notFoundf("snapshot %q not found on %q", snapshot, name)
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
	return notFoundf("snapshot %q not found on %q", snapshot, name)
}
