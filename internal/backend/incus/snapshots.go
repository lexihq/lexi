package incus

import (
	"context"
	"fmt"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

func (b *incusBackend) ListSnapshots(_ context.Context, name string) ([]backend.Snapshot, error) {
	snaps, err := b.srv.GetInstanceSnapshots(name)
	if err != nil {
		return nil, fmt.Errorf("list snapshots of %q: %w", name, mapErr(err))
	}
	out := make([]backend.Snapshot, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, backend.Snapshot{
			Name:      snapshotShortName(s.Name),
			CreatedAt: s.CreatedAt,
			Stateful:  s.Stateful,
		})
	}
	return out, nil
}

func (b *incusBackend) CreateSnapshot(ctx context.Context, name, snapshot string) error {
	op, err := b.srv.CreateInstanceSnapshot(name, api.InstanceSnapshotsPost{Name: snapshot})
	return waitOp(ctx, op, err, "snapshot %q of %q", snapshot, name)
}

func (b *incusBackend) RestoreSnapshot(ctx context.Context, name, snapshot string) error {
	// GET-then-PUT preserves the instance config; Restore triggers the rollback.
	return b.mutateInstance(ctx, name, func(put *api.InstancePut) {
		put.Restore = snapshot
	}, "restore %q on %q", snapshot, name)
}

func (b *incusBackend) DeleteSnapshot(ctx context.Context, name, snapshot string) error {
	op, err := b.srv.DeleteInstanceSnapshot(name, snapshot)
	return waitOp(ctx, op, err, "delete snapshot %q of %q", snapshot, name)
}

func snapshotShortName(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}
