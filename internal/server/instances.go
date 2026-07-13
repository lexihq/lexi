package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/metrics"
	"github.com/lexihq/lexi/internal/ui"
)

// instanceTrends reads recent usage history per instance from the metrics
// store for the list sparklines and the summary readout. It's a cheap
// in-memory read (no backend driver call); instances without enough retained
// samples are simply absent from the map, so their row omits the sparkline.
func (h handlers) instanceTrends(ctx context.Context, instances []backend.Instance) map[string]ui.InstanceTrend {
	out := make(map[string]ui.InstanceTrend, len(instances))
	for _, inst := range instances {
		samples := h.samples.Series(metrics.Key(ctx, inst.Name))
		if len(samples) < 2 {
			continue
		}
		trend := ui.InstanceTrend{CPU: make([]float64, len(samples))}
		for i, s := range samples {
			trend.CPU[i] = s.CPUPercent
			if s.MemoryTotal > 0 {
				trend.MemPercent = append(trend.MemPercent, float64(s.MemoryUsage)/float64(s.MemoryTotal)*100)
			}
		}
		// The two sparklines render side by side over the same 80px; a memory
		// series with holes (samples missing totals) would silently cover a
		// different time window than the CPU one, so drop it instead.
		if len(trend.MemPercent) != len(trend.CPU) {
			trend.MemPercent = nil
		}
		last := samples[len(samples)-1]
		trend.CPUNow = last.CPUPercent
		trend.MemUsed = last.MemoryUsage
		trend.MemTotal = last.MemoryTotal
		out[inst.Name] = trend
	}
	return out
}

func (h handlers) list(w http.ResponseWriter, r *http.Request) {
	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// The catalog and optional selectors feed the create-instance dialog (and
	// pools double as the move-to-pool datalist). All best-effort: a listing
	// failure just drops that selector, never the instance list.
	caps := h.backend.Capabilities(r.Context())
	images, profiles, pools, networks := h.createDialogData(r.Context(), caps)

	// Row sparklines read from the metrics store; carry them via context so the
	// page/table/row signatures stay unchanged (like the sidebar list).
	r = r.WithContext(ui.WithInstanceTrends(r.Context(), h.instanceTrends(r.Context(), instances)))
	// The list already has the instances the sidebar needs; reuse them.
	h.renderWithSidebar(w, r, http.StatusOK, instances, ui.InstancesPage(caps, instances, images, profiles, pools, networks, h.overview(r.Context(), caps)))
}

// overview gathers the cluster-band data for the instance list, all
// best-effort: a failed fetch hides that tile (Has* false) rather than
// failing the page the operator is trying to reach.
func (h handlers) overview(ctx context.Context, caps backend.Capabilities) ui.Overview {
	var ov ui.Overview
	if caps.ServerAdmin {
		if so, err := h.backend.GetServerOverview(ctx); err == nil {
			ov.CPUThreads = so.CPUThreads
			ov.MemoryUsed = so.MemoryUsed
			ov.MemoryTotal = so.MemoryTotal
			ov.HasHost = true
		} else {
			slog.Warn("overview: server overview", "err", err)
		}
		if warnings, err := h.backend.ListWarnings(ctx); err == nil {
			for _, warning := range warnings {
				if warning.Status == backend.WarningNew {
					ov.NewWarnings++
				}
			}
			ov.HasWarnings = true
		} else {
			slog.Warn("overview: list warnings", "err", err)
		}
	}
	if caps.Operations {
		if ops, err := h.backend.ListOperations(ctx); err == nil {
			for _, op := range ops {
				if op.Status == backend.OpRunning {
					ov.RunningTasks++
				}
			}
			ov.HasTasks = true
		} else {
			slog.Warn("overview: list operations", "err", err)
		}
	}
	return ov
}

// instancesPartial renders just the instances table fragment for the list's
// idle auto-refresh (bulk-actions.js polls this every 15s while nothing
// is selected, so live status and CPU sparklines update without a manual
// reload). It mirrors list's trend injection but emits only the table fragment.
func (h handlers) instancesPartial(w http.ResponseWriter, r *http.Request) {
	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	r = r.WithContext(ui.WithInstanceTrends(r.Context(), h.instanceTrends(r.Context(), instances)))
	// The migrate menu reads the remote switcher context; inject it here too so
	// the idle poll doesn't strip "Migrate…" from stopped rows (renderWithSidebar
	// only adds it on the full page). Likewise the project scope, which the
	// destructive confirm prompts name via scopeSuffix.
	r = r.WithContext(h.withRemoteSwitcher(r.Context()))
	r = r.WithContext(ui.WithProjectSwitcher(r.Context(), nil, backend.ProjectFromContext(r.Context())))
	h.render(w, r, http.StatusOK, ui.InstancesTable(h.backend.Capabilities(r.Context()), instances))
}

// sidebar renders the self-refreshing instance list for the shell sidebar. The
// active param (the currently-viewed instance name) drives the highlight.
func (h handlers) sidebar(w http.ResponseWriter, r *http.Request) {
	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.SidebarInstances(instances, r.URL.Query().Get("active")))
}

func (h handlers) detail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	snapshots, err := h.backend.ListSnapshots(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// The Summary tab's "Usage now" readout reads from the metrics store, same
	// as the list sparklines.
	r = r.WithContext(ui.WithInstanceTrends(r.Context(), h.instanceTrends(r.Context(), []backend.Instance{inst})))
	tab := r.URL.Query().Get("tab")
	// A tab click is an explicit (non-boosted) HTMX request and gets just the
	// swappable body. A boosted navigation (clicking the instance in the sidebar
	// or list) carries HX-Boosted and must get the full page so the shell's
	// #content swap finds the whole content region.
	if isHTMX(r) && !isBoosted(r) {
		h.render(w, r, http.StatusOK, ui.InstanceBody(h.backend.Capabilities(r.Context()), inst, snapshots, tab))
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.InstancePage(h.backend.Capabilities(r.Context()), inst, snapshots, tab))
}

func (h handlers) start(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, "Started", func(name string) error { return h.backend.StartInstance(r.Context(), name) })
}

func (h handlers) stop(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, "Stopped", func(name string) error { return h.backend.StopInstance(r.Context(), name) })
}

func (h handlers) restart(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, "Restarted", func(name string) error { return h.backend.RestartInstance(r.Context(), name) })
}

func (h handlers) pause(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, "Paused", func(name string) error { return h.backend.PauseInstance(r.Context(), name) })
}

func (h handlers) resume(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, "Resumed", func(name string) error { return h.backend.ResumeInstance(r.Context(), name) })
}

func (h handlers) delete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	// Same server-side typed-name gate as rebuild: the dialog's confirm input
	// travels with the request (hx-include in deleteAttrs), so a bare POST
	// can't fire the destructive path.
	if strings.TrimSpace(r.Form.Get("confirm")) != name {
		h.renderError(w, r, http.StatusBadRequest, "type the instance name to confirm the deletion")
		return
	}
	if err := h.backend.DeleteInstance(r.Context(), name); err != nil {
		h.fail(w, r, err)
		return
	}
	if isHTMX(r) {
		writeHTML(w, http.StatusOK)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// bulkOps maps each bulk action key to its per-instance backend call. The set of
// actions, their toast verbs, capability gating, and button rendering live in
// ui.BulkActions (the single source of truth); this map only supplies behavior,
// keyed by the same Key. TestBulkOpsMatchRegistry asserts the two stay in
// lockstep so the bar and the handler can't drift. snap is the shared snapshot
// name for the batch (used only by snapshot; ignored otherwise).
var bulkOps = map[string]func(h handlers, r *http.Request, name, snap string) error{
	"start": func(h handlers, r *http.Request, name, _ string) error {
		return h.backend.StartInstance(r.Context(), name)
	},
	"stop": func(h handlers, r *http.Request, name, _ string) error {
		return h.backend.StopInstance(r.Context(), name)
	},
	"restart": func(h handlers, r *http.Request, name, _ string) error {
		return h.backend.RestartInstance(r.Context(), name)
	},
	"snapshot": func(h handlers, r *http.Request, name, snap string) error {
		return h.backend.CreateSnapshot(r.Context(), name, snap, backend.SnapshotOptions{})
	},
	"delete": func(h handlers, r *http.Request, name, _ string) error {
		return h.backend.DeleteInstance(r.Context(), name)
	},
}

// bulk applies one lifecycle action to every selected instance by looping the
// existing per-instance backend methods (there is no bulk driver primitive). It
// collects per-instance failures into the summary toast rather than aborting on
// the first, then re-renders the table fragment reflecting the new state.
func (h handlers) bulk(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	action := r.Form.Get("action")
	names := r.Form["name"]
	if len(names) == 0 {
		h.renderError(w, r, http.StatusBadRequest, "select at least one instance")
		return
	}
	meta, ok := ui.BulkActionByKey(action)
	op := bulkOps[action]
	if !ok || op == nil {
		h.renderError(w, r, http.StatusBadRequest, "unknown bulk action")
		return
	}
	// Defensive at the boundary: the UI hides actions the tier doesn't support,
	// but a crafted request could still post one — reject it once here instead of
	// failing per instance.
	if meta.Needs != nil && !meta.Needs(h.backend.Capabilities(r.Context())) {
		h.renderError(w, r, http.StatusUnprocessableEntity, meta.Label+" is not supported here")
		return
	}
	// One shared snapshot name for the batch (each instance gets its own snapshot
	// under it); computed once so a whole bulk snapshot is easy to spot later.
	snapName := "manual-" + time.Now().UTC().Format("20060102-150405")

	// Apply to each instance concurrently (bounded) — the per-instance backend
	// calls are independent daemon round-trips, so a serial loop made the request
	// block for ~N × per-op latency. Record failures per index so the summary
	// order stays deterministic regardless of completion order.
	const maxConcurrent = 8
	sem := make(chan struct{}, maxConcurrent)
	failedAt := make([]bool, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			// Contain a panic in one instance's op: mark it failed and log,
			// rather than letting the panic unwind the goroutine and crash the
			// whole process (taking every other in-flight request with it).
			defer func() {
				if rec := recover(); rec != nil {
					failedAt[i] = true
					slog.Error("bulk action panicked", "action", action, "instance", name, "panic", rec)
				}
			}()
			if err := op(h, r, name, snapName); err != nil {
				failedAt[i] = true
				slog.Warn("bulk action failed", "action", action, "instance", name, "err", err)
			}
		}()
	}
	wg.Wait()

	var failed []string
	for i, name := range names {
		if failedAt[i] {
			failed = append(failed, name)
		}
	}

	// The summary toast is an HTMX out-of-band swap; a non-HTMX client gets the
	// redirect-after-POST every other mutation handler uses (see delete/rescue).
	if !isHTMX(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	msg := fmt.Sprintf("%s %d of %d", meta.Verb, len(names)-len(failed), len(names))
	if len(failed) > 0 {
		msg += " — failed: " + strings.Join(failed, ", ")
	}
	r = r.WithContext(ui.WithInstanceTrends(r.Context(), h.instanceTrends(r.Context(), instances)))
	r = r.WithContext(h.withRemoteSwitcher(r.Context()))
	h.renderWithToast(w, r, http.StatusOK, ui.InstancesTable(h.backend.Capabilities(r.Context()), instances), msg)
}

// rescue creates a one-click recovery checkpoint for a misbehaving instance: it
// snapshots the current state (a restore point) and then freezes the instance so
// it stops doing work and can be inspected (logs/console/config tabs) without
// racing further. Snapshot-then-pause so the checkpoint captures the live state.
func (h handlers) rescue(w http.ResponseWriter, r *http.Request) {
	// The UI gates Rescue on Pause && Snapshots; re-check here so a crafted
	// request can't leave a partial mutation (an orphan snapshot on a tier
	// that then fails the pause) — same defense the bulk endpoint applies.
	if caps := h.backend.Capabilities(r.Context()); !caps.Pause || !caps.Snapshots {
		h.fail(w, r, fmt.Errorf("rescue is not supported here: %w", backend.ErrUnsupported))
		return
	}
	name := r.PathValue("name")
	snap := "rescue-" + time.Now().UTC().Format("20060102-150405")
	if err := h.backend.CreateSnapshot(r.Context(), name, snap, backend.SnapshotOptions{}); err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.backend.PauseInstance(r.Context(), name); err != nil {
		h.fail(w, r, err)
		return
	}
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if isHTMX(r) {
		h.renderWithToast(w, r, http.StatusOK, ui.InstanceHeader(h.backend.Capabilities(r.Context()), inst),
			"Rescued: snapshot "+snap+" created and instance frozen for inspection")
		return
	}
	redirectToInstance(w, name)
}

func (h handlers) clone(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dst := strings.TrimSpace(r.Form.Get("dst"))
	if dst == "" {
		h.renderError(w, r, http.StatusBadRequest, "clone name is required")
		return
	}
	if err := h.backend.CloneInstance(r.Context(), r.PathValue("name"), dst); err != nil {
		h.fail(w, r, err)
		return
	}
	inst, err := h.backend.GetInstance(r.Context(), dst)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.InstanceRow(h.backend.Capabilities(r.Context()), inst))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
