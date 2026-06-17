package fake

import (
	"context"
	"fmt"
	"sort"

	"github.com/lexihq/lexi/internal/backend"
)

// MigrateInstance relocates a stopped instance to another fake daemon's
// default project. Guards mirror the seam contract: running → ErrInvalid,
// unknown target → ErrNotFound, name taken on the target → ErrConflict; the
// source is untouched on any failure.
func (f *Fake) MigrateInstance(ctx context.Context, name, targetRemote, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	if in.Status != "Stopped" {
		return invalid("instance %q must be stopped before migrating", name)
	}
	if _, ok := f.remotes[targetRemote]; !ok {
		return notFoundf("remote %q", targetRemote)
	}
	dstName := newName
	if dstName == "" {
		dstName = name
	}
	dst := f.remotes[targetRemote].spaceFor("default")
	if _, taken := dst.instances[dstName]; taken {
		return conflict("instance %q already exists on %q", dstName, targetRemote)
	}

	delete(sp.instances, name)
	in.Name = dstName
	dst.instances[dstName] = in
	f.logOp(sp, fmt.Sprintf("Migrating instance %q to %q", name, targetRemote))
	f.logOp(dst, fmt.Sprintf("Received instance %q from migration", dstName))
	return nil
}

// ListRemotes returns the fake's daemons, marking the request's selection
// (default: "local") as Current. Sorted by name for a stable switcher.
func (f *Fake) ListRemotes(ctx context.Context) ([]backend.Remote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	current := remoteOf(ctx)
	names := make([]string, 0, len(f.remotes))
	for name := range f.remotes {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]backend.Remote, 0, len(names))
	for _, name := range names {
		out = append(out, backend.Remote{Name: name, Addr: "fake:" + name, Current: name == current})
	}
	return out, nil
}
