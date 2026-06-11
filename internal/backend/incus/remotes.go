package incus

import (
	"context"
	"sort"

	"github.com/adam/lxcon/internal/backend"
)

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
