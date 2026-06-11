package fake

import (
	"context"
	"sort"

	"github.com/adam/lxcon/internal/backend"
)

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
