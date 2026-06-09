package ui

import (
	"context"

	"github.com/adam/lxcon/internal/backend"
)

// sidebarCtxKey keys the instance list that full-page handlers preload so the
// shell renders the sidebar list server-side. With hx-boost swapping the
// content region in place, a server-rendered list means navigation never
// flashes an empty sidebar. It is layout-wide data, so it travels via context
// rather than threading through every page component's signature.
type sidebarCtxKey struct{}

// WithSidebarInstances returns a context carrying the instance list the shell
// sidebar should render on first paint.
func WithSidebarInstances(ctx context.Context, instances []backend.Instance) context.Context {
	return context.WithValue(ctx, sidebarCtxKey{}, instances)
}

// sidebarInstancesFrom returns the preloaded sidebar instance list, or nil when
// a render path (e.g. a unit test rendering a page directly) didn't set one —
// in which case the sidebar's poll fills it in shortly after load.
func sidebarInstancesFrom(ctx context.Context) []backend.Instance {
	instances, ok := ctx.Value(sidebarCtxKey{}).([]backend.Instance)
	if !ok {
		return nil
	}
	return instances
}
