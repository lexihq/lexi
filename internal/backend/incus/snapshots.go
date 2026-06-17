package incus

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

func (b *incusBackend) ListSnapshots(ctx context.Context, name string) ([]backend.Snapshot, error) {
	snaps, err := b.project(ctx).GetInstanceSnapshots(name)
	if err != nil {
		return nil, fmt.Errorf("list snapshots of %q: %w", name, mapErr(err))
	}
	out := make([]backend.Snapshot, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, backend.Snapshot{
			Name:      snapshotShortName(s.Name),
			CreatedAt: s.CreatedAt,
			Stateful:  s.Stateful,
			ExpiresAt: s.ExpiresAt,
		})
	}
	return out, nil
}

func (b *incusBackend) CreateSnapshot(ctx context.Context, name, snapshot string, opts backend.SnapshotOptions) error {
	post := api.InstanceSnapshotsPost{Name: snapshot, Stateful: opts.Stateful}
	if !opts.ExpiresAt.IsZero() {
		t := opts.ExpiresAt
		post.ExpiresAt = &t
	}
	op, err := b.project(ctx).CreateInstanceSnapshot(name, post)
	return waitOp(ctx, op, err, "snapshot %q of %q", snapshot, name)
}

func (b *incusBackend) RenameSnapshot(ctx context.Context, name, snapshot, newName string) error {
	op, err := b.project(ctx).RenameInstanceSnapshot(name, snapshot, api.InstanceSnapshotPost{Name: newName})
	return waitOp(ctx, op, err, "rename snapshot %q to %q on %q", snapshot, newName, name)
}

func (b *incusBackend) UpdateSnapshotExpiry(ctx context.Context, name, snapshot string, expiresAt time.Time) error {
	_, etag, err := b.project(ctx).GetInstanceSnapshot(name, snapshot)
	if err != nil {
		return fmt.Errorf("get snapshot %q of %q: %w", snapshot, name, mapErr(err))
	}
	op, err := b.project(ctx).UpdateInstanceSnapshot(name, snapshot, api.InstanceSnapshotPut{ExpiresAt: expiresAt}, etag)
	return waitOp(ctx, op, err, "update expiry of %q on %q", snapshot, name)
}

func (b *incusBackend) RestoreSnapshot(ctx context.Context, name, snapshot string) error {
	// GET-then-PUT preserves the instance config; Restore triggers the rollback.
	return b.mutateInstance(ctx, name, func(put *api.InstancePut) {
		put.Restore = snapshot
	}, "restore %q on %q", snapshot, name)
}

func (b *incusBackend) DeleteSnapshot(ctx context.Context, name, snapshot string) error {
	op, err := b.project(ctx).DeleteInstanceSnapshot(name, snapshot)
	return waitOp(ctx, op, err, "delete snapshot %q of %q", snapshot, name)
}

func (b *incusBackend) GetSnapshotSchedule(ctx context.Context, name string) (backend.SnapshotSchedule, error) {
	inst, _, err := b.project(ctx).GetInstance(name)
	if err != nil {
		return backend.SnapshotSchedule{}, fmt.Errorf("get instance %q: %w", name, mapErr(err))
	}
	return backend.SnapshotSchedule{
		Schedule: inst.Config["snapshots.schedule"],
		Expiry:   inst.Config["snapshots.expiry"],
		Pattern:  inst.Config["snapshots.pattern"],
	}, nil
}

func (b *incusBackend) SetSnapshotSchedule(ctx context.Context, name string, s backend.SnapshotSchedule) error {
	return b.mutateInstance(ctx, name, func(put *api.InstancePut) {
		if put.Config == nil {
			put.Config = map[string]string{}
		}
		setOrDelete(put.Config, "snapshots.schedule", s.Schedule)
		setOrDelete(put.Config, "snapshots.expiry", s.Expiry)
		setOrDelete(put.Config, "snapshots.pattern", s.Pattern)
	}, "set snapshot schedule on %q", name)
}

func snapshotShortName(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}
