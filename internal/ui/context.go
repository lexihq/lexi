package ui

import (
	"context"

	"github.com/lexihq/lexi/internal/backend"
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

// projectSwitcherCtxKey keys the project list + current selection the shell
// preloads for the sidebar switcher — layout-wide data, like the sidebar
// instance list.
type projectSwitcherCtxKey struct{}

type projectSwitcher struct {
	Projects []backend.Project
	Current  string
}

// WithProjectSwitcher returns a context carrying the switcher state.
func WithProjectSwitcher(ctx context.Context, projects []backend.Project, current string) context.Context {
	return context.WithValue(ctx, projectSwitcherCtxKey{}, projectSwitcher{Projects: projects, Current: current})
}

// projectSwitcherFrom returns the preloaded switcher state; an absent value
// (render paths that didn't set one) hides the switcher.
func projectSwitcherFrom(ctx context.Context) projectSwitcher {
	if ps, ok := ctx.Value(projectSwitcherCtxKey{}).(projectSwitcher); ok {
		return ps
	}
	return projectSwitcher{}
}

// remoteSwitcherCtxKey keys the remote list the shell preloads for the
// sidebar switcher — layout-wide data, like the project switcher.
type remoteSwitcherCtxKey struct{}

// WithRemoteSwitcher returns a context carrying the switcher's remote list
// (each entry's Current flag marks the selection).
func WithRemoteSwitcher(ctx context.Context, remotes []backend.Remote) context.Context {
	return context.WithValue(ctx, remoteSwitcherCtxKey{}, remotes)
}

// remoteSwitcherFrom returns the preloaded remote list; an absent value
// (render paths that didn't set one) hides the switcher.
func remoteSwitcherFrom(ctx context.Context) []backend.Remote {
	if remotes, ok := ctx.Value(remoteSwitcherCtxKey{}).([]backend.Remote); ok {
		return remotes
	}
	return nil
}

// scopeSuffix names the active project/remote for destructive confirm prompts,
// e.g. " in project “staging” on “backup-host”". It stays empty for the common
// single-scope setup (default project, one remote) so short prompts stay short;
// it only appears when the current scope is not the default one. Row-swap and
// partial render paths must inject the project switcher (current name is
// enough) or the clause silently disappears from re-rendered prompts.
func scopeSuffix(ctx context.Context) string {
	var s string
	if ps := projectSwitcherFrom(ctx); ps.Current != "" && ps.Current != "default" {
		s = " in project “" + ps.Current + "”"
	}
	if remotes := remoteSwitcherFrom(ctx); len(remotes) > 1 {
		for _, r := range remotes {
			if r.Current {
				return s + " on “" + r.Name + "”"
			}
		}
	}
	return s
}

// instanceTrendsCtxKey keys the per-instance usage-history data the list (and
// the detail Summary) preloads from the metrics store. Like the sidebar list it
// is row-level data rendered by InstanceRow, so it travels via context rather
// than widening the signature of every handler that re-renders a row.
type instanceTrendsCtxKey struct{}

// InstanceTrend is one instance's recent usage from the metrics store: CPU%
// and memory-used% histories (oldest first) for the sparklines, plus the
// latest absolute values for the text readout. Zero value = no samples.
type InstanceTrend struct {
	CPU        []float64 // CPU% history, 0–100
	MemPercent []float64 // memory used % of total, 0–100; empty when total unknown
	CPUNow     float64
	MemUsed    int64
	MemTotal   int64
}

// WithInstanceTrends returns a context carrying recent usage history per
// instance name for the list sparklines and summary readout.
func WithInstanceTrends(ctx context.Context, trends map[string]InstanceTrend) context.Context {
	return context.WithValue(ctx, instanceTrendsCtxKey{}, trends)
}

// instanceTrendFrom returns the preloaded usage history for one instance, or
// the zero value when a render path (a single-row swap, a unit test) didn't
// set any — in which case the row simply omits its sparkline.
func instanceTrendFrom(ctx context.Context, name string) InstanceTrend {
	if trends, ok := ctx.Value(instanceTrendsCtxKey{}).(map[string]InstanceTrend); ok {
		return trends[name]
	}
	return InstanceTrend{}
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
