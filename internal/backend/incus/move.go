package incus

import (
	"context"
	"fmt"

	"github.com/lxc/incus/v6/shared/api"

	"github.com/lexihq/lexi/internal/backend"
)

func (b *incusBackend) RenameInstance(ctx context.Context, name, newName string) error {
	// Rename failures arrive as plain operation errors with no HTTP status,
	// so the daemon's name validation can't be mapped after the fact (same
	// pre-check as RenameProject; no apiNameEnds — single-char instance
	// names are legal).
	if !validBaseName(newName) {
		return fmt.Errorf("invalid instance name %q: %w", newName, backend.ErrInvalid)
	}
	op, err := b.project(ctx).RenameInstance(name, api.InstancePost{Name: newName})
	return waitOp(ctx, op, err, "rename instance %q to %q", name, newName)
}

func (b *incusBackend) MoveInstance(ctx context.Context, name, pool string) error {
	// Migration must be true even for a local cross-pool move: the client rejects
	// MigrateInstance with Migration=false ("Can't ask for a rename through
	// MigrateInstance"). No Target → local pull-mode move (matches `incus move
	// <name> <name> --storage <pool>`).
	op, err := b.project(ctx).MigrateInstance(name, api.InstancePost{Name: name, Pool: pool, Migration: true})
	return waitOp(ctx, op, err, "move instance %q to pool %q", name, pool)
}
