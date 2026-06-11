package fake

import (
	"context"
	"fmt"
	"time"

	"github.com/adam/lxcon/internal/backend"
)

func (f *Fake) ListSnapshots(ctx context.Context, name string) ([]backend.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return nil, notFound(name)
	}
	return append([]backend.Snapshot(nil), in.snapshots...), nil
}

func (f *Fake) CreateSnapshot(ctx context.Context, name, snapshot string, opts backend.SnapshotOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	for _, s := range in.snapshots {
		if s.Name == snapshot {
			return conflict("snapshot %q already exists on %q", snapshot, name)
		}
	}
	in.snapshots = append(in.snapshots, backend.Snapshot{
		Name: snapshot, CreatedAt: f.now(), Stateful: opts.Stateful, ExpiresAt: opts.ExpiresAt,
	})
	f.logOp(sp, fmt.Sprintf("Creating snapshot %q of %q", snapshot, name))
	return nil
}

func (f *Fake) RenameSnapshot(ctx context.Context, name, snapshot, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	idx := -1
	for i, s := range in.snapshots {
		if s.Name == newName {
			return conflict("snapshot %q already exists on %q", newName, name)
		}
		if s.Name == snapshot {
			idx = i
		}
	}
	if idx < 0 {
		return notFoundf("snapshot %q not found on %q", snapshot, name)
	}
	in.snapshots[idx].Name = newName
	return nil
}

func (f *Fake) UpdateSnapshotExpiry(ctx context.Context, name, snapshot string, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	for i, s := range in.snapshots {
		if s.Name == snapshot {
			in.snapshots[i].ExpiresAt = expiresAt
			return nil
		}
	}
	return notFoundf("snapshot %q not found on %q", snapshot, name)
}

func (f *Fake) GetSnapshotSchedule(ctx context.Context, name string) (backend.SnapshotSchedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return backend.SnapshotSchedule{}, notFound(name)
	}
	return backend.SnapshotSchedule{
		Schedule: in.config["snapshots.schedule"],
		Expiry:   in.config["snapshots.expiry"],
		Pattern:  in.config["snapshots.pattern"],
	}, nil
}

func (f *Fake) SetSnapshotSchedule(ctx context.Context, name string, s backend.SnapshotSchedule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	if in.config == nil {
		in.config = map[string]string{}
	}
	setOrDeleteKey(in.config, "snapshots.schedule", s.Schedule)
	setOrDeleteKey(in.config, "snapshots.expiry", s.Expiry)
	setOrDeleteKey(in.config, "snapshots.pattern", s.Pattern)
	return nil
}

// setOrDeleteKey writes val under key, or deletes the key when val is empty.
func setOrDeleteKey(m map[string]string, key, val string) {
	if val == "" {
		delete(m, key)
		return
	}
	m[key] = val
}

func (f *Fake) RestoreSnapshot(ctx context.Context, name, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	for _, s := range in.snapshots {
		if s.Name == snapshot {
			f.logOp(sp, fmt.Sprintf("Restoring snapshot %q of %q", snapshot, name))
			return nil
		}
	}
	return notFoundf("snapshot %q not found on %q", snapshot, name)
}

func (f *Fake) DeleteSnapshot(ctx context.Context, name, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	for i, s := range in.snapshots {
		if s.Name == snapshot {
			in.snapshots = append(in.snapshots[:i], in.snapshots[i+1:]...)
			f.logOp(sp, fmt.Sprintf("Deleting snapshot %q of %q", snapshot, name))
			return nil
		}
	}
	return notFoundf("snapshot %q not found on %q", snapshot, name)
}
