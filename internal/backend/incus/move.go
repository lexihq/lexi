package incus

import (
	"context"

	"github.com/lxc/incus/v6/shared/api"
)

func (b *incusBackend) RenameInstance(ctx context.Context, name, newName string) error {
	op, err := b.srv.RenameInstance(name, api.InstancePost{Name: newName})
	return waitOp(ctx, op, err, "rename instance %q to %q", name, newName)
}

func (b *incusBackend) MoveInstance(ctx context.Context, name, pool string) error {
	op, err := b.srv.MigrateInstance(name, api.InstancePost{Name: name, Pool: pool})
	return waitOp(ctx, op, err, "move instance %q to pool %q", name, pool)
}
