package incus

import (
	"context"
	"fmt"
	"sort"

	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"

	"github.com/lexihq/lexi/internal/backend"
)

// MigrateInstance moves a stopped instance to another reachable remote: a
// pull-mode server-to-server copy, then the source is deleted only after the
// copy succeeds, so the source survives any failure. The target lands in the
// remote's default project. target == source is allowed here (a copy-rename
// on one daemon — the integration tests exercise the real transfer machinery
// through it); the HTTP layer rejects it for the UI.
func (b *incusBackend) MigrateInstance(ctx context.Context, name, targetRemote, newName string) error {
	dst, ok := b.remotes[targetRemote]
	if !ok {
		return fmt.Errorf("remote %q: %w", targetRemote, backend.ErrNotFound)
	}
	src := b.project(ctx)

	inst, _, err := src.GetInstance(name)
	if err != nil {
		return fmt.Errorf("migrate %q: %w", name, mapErr(err))
	}
	if inst.StatusCode != api.Stopped {
		return fmt.Errorf("instance %q must be stopped before migrating: %w", name, backend.ErrInvalid)
	}

	args := &incusclient.InstanceCopyArgs{Mode: "pull"}
	if newName != "" {
		args.Name = newName
	}
	op, err := dst.srv.CopyInstance(src, *inst, args)
	if err != nil {
		return fmt.Errorf("migrate %q to %q: %w", name, targetRemote, mapErr(err))
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("migrate %q to %q: %w", name, targetRemote, mapErr(err))
	}

	delOp, err := src.DeleteInstance(name)
	if err != nil {
		return fmt.Errorf("remove migrated source %q: %w", name, mapErr(err))
	}
	if err := delOp.Wait(); err != nil {
		return fmt.Errorf("remove migrated source %q: %w", name, mapErr(err))
	}
	return nil
}

// ListRemotes reports the remotes that were reachable at startup, sorted by
// name, marking the request's selection (default when unset) as Current.
func (b *incusBackend) ListRemotes(ctx context.Context) ([]backend.Remote, error) {
	current := backend.RemoteFromContext(ctx)
	if _, ok := b.remotes[current]; !ok {
		current = b.remoteName
	}

	names := make([]string, 0, len(b.remotes))
	for name := range b.remotes {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]backend.Remote, 0, len(names))
	for _, name := range names {
		out = append(out, backend.Remote{Name: name, Addr: b.remotes[name].addr, Current: name == current})
	}
	return out, nil
}
