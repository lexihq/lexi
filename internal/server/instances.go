package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/metrics"
	"github.com/lexihq/lexi/internal/ui"
)

// instanceTrends reads recent CPU% history per instance from the metrics store
// for the list sparklines. It's a cheap in-memory read (no backend driver call);
// instances without enough retained samples are simply absent from the map, so
// their row omits the sparkline.
func (h handlers) instanceTrends(ctx context.Context, instances []backend.Instance) map[string][]float64 {
	out := make(map[string][]float64, len(instances))
	for _, inst := range instances {
		samples := h.samples.Series(metrics.Key(ctx, inst.Name))
		if len(samples) < 2 {
			continue
		}
		cpu := make([]float64, len(samples))
		for i, s := range samples {
			cpu[i] = s.CPUPercent
		}
		out[inst.Name] = cpu
	}
	return out
}

func (h handlers) list(w http.ResponseWriter, r *http.Request) {
	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	h.renderWithSidebar(w, r, http.StatusOK, instances, ui.InstancesPage(caps, instances, images, profiles, pools, networks))
}

// instancesPartial renders just the instances table fragment for the list's
// idle auto-refresh (bulk-actions.js polls this every few seconds while nothing
// is selected, so live status and CPU sparklines update without a manual
// reload). It mirrors list's trend injection but emits only the table fragment.
func (h handlers) instancesPartial(w http.ResponseWriter, r *http.Request) {
	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	r = r.WithContext(ui.WithInstanceTrends(r.Context(), h.instanceTrends(r.Context(), instances)))
	h.render(w, r, http.StatusOK, ui.InstancesTable(h.backend.Capabilities(r.Context()), instances))
}

// sidebar renders the self-refreshing instance list for the shell sidebar. The
// active param (the currently-viewed instance name) drives the highlight.
func (h handlers) sidebar(w http.ResponseWriter, r *http.Request) {
	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.render(w, r, http.StatusOK, ui.SidebarInstances(instances, r.URL.Query().Get("active")))
}

func (h handlers) detail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	snapshots, err := h.backend.ListSnapshots(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	var profiles []backend.Profile
	if h.backend.Capabilities(r.Context()).Profiles {
		profiles, err = h.backend.ListProfiles(r.Context())
		if err != nil {
			http.Error(w, err.Error(), statusFor(err))
			return
		}
	}

	tab := r.URL.Query().Get("tab")
	// A tab click is an explicit (non-boosted) HTMX request and gets just the
	// swappable body. A boosted navigation (clicking the instance in the sidebar
	// or list) carries HX-Boosted and must get the full page so the shell's
	// #content swap finds the whole content region.
	if isHTMX(r) && !isBoosted(r) {
		h.render(w, r, http.StatusOK, ui.InstanceBody(h.backend.Capabilities(r.Context()), inst, snapshots, profiles, tab))
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.InstancePage(h.backend.Capabilities(r.Context()), inst, snapshots, profiles, tab))
}

func (h handlers) start(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, func(name string) error { return h.backend.StartInstance(r.Context(), name) })
}

func (h handlers) stop(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, func(name string) error { return h.backend.StopInstance(r.Context(), name) })
}

func (h handlers) restart(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, func(name string) error { return h.backend.RestartInstance(r.Context(), name) })
}

func (h handlers) pause(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, func(name string) error { return h.backend.PauseInstance(r.Context(), name) })
}

func (h handlers) resume(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, func(name string) error { return h.backend.ResumeInstance(r.Context(), name) })
}

func (h handlers) delete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.backend.DeleteInstance(r.Context(), name); err != nil {
		h.fail(w, err)
		return
	}
	if isHTMX(r) {
		writeHTML(w, http.StatusOK)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// bulkVerbs maps each supported bulk action to the backend call and the past-
// tense verb used in the summary toast. It's the single source of truth for
// which bulk actions exist (the UI bar renders a button per entry's intent).
var bulkVerbs = map[string]string{
	"start":    "Started",
	"stop":     "Stopped",
	"restart":  "Restarted",
	"snapshot": "Snapshotted",
	"delete":   "Deleted",
}

// bulk applies one lifecycle action to every selected instance by looping the
// existing per-instance backend methods (there is no bulk driver primitive). It
// collects per-instance failures into the summary toast rather than aborting on
// the first, then re-renders the table fragment reflecting the new state.
func (h handlers) bulk(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, err.Error())
		return
	}
	action := r.Form.Get("action")
	names := r.Form["name"]
	if len(names) == 0 {
		h.renderError(w, http.StatusBadRequest, "select at least one instance")
		return
	}
	verb, ok := bulkVerbs[action]
	if !ok {
		h.renderError(w, http.StatusBadRequest, "unknown bulk action")
		return
	}
	// One shared snapshot name for the batch (each instance gets its own snapshot
	// under it); computed once so a whole bulk snapshot is easy to spot later.
	snapName := "manual-" + time.Now().UTC().Format("20060102-150405")
	op := func(name string) error {
		switch action {
		case "start":
			return h.backend.StartInstance(r.Context(), name)
		case "stop":
			return h.backend.StopInstance(r.Context(), name)
		case "restart":
			return h.backend.RestartInstance(r.Context(), name)
		case "snapshot":
			return h.backend.CreateSnapshot(r.Context(), name, snapName, backend.SnapshotOptions{})
		default: // delete
			return h.backend.DeleteInstance(r.Context(), name)
		}
	}

	var failed []string
	for _, name := range names {
		if err := op(name); err != nil {
			failed = append(failed, name)
			slog.Warn("bulk action failed", "action", action, "instance", name, "err", err)
		}
	}

	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	msg := fmt.Sprintf("%s %d of %d", verb, len(names)-len(failed), len(names))
	if len(failed) > 0 {
		msg += " — failed: " + strings.Join(failed, ", ")
	}
	r = r.WithContext(ui.WithInstanceTrends(r.Context(), h.instanceTrends(r.Context(), instances)))
	h.renderWithToast(w, r, http.StatusOK, ui.InstancesTable(h.backend.Capabilities(r.Context()), instances), msg)
}

// rescue creates a one-click recovery checkpoint for a misbehaving instance: it
// snapshots the current state (a restore point) and then freezes the instance so
// it stops doing work and can be inspected (logs/console/config tabs) without
// racing further. Snapshot-then-pause so the checkpoint captures the live state.
func (h handlers) rescue(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	snap := "rescue-" + time.Now().UTC().Format("20060102-150405")
	if err := h.backend.CreateSnapshot(r.Context(), name, snap, backend.SnapshotOptions{}); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.backend.PauseInstance(r.Context(), name); err != nil {
		h.fail(w, err)
		return
	}
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		h.fail(w, err)
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
		h.renderError(w, http.StatusBadRequest, "clone name is required")
		return
	}
	if err := h.backend.CloneInstance(r.Context(), r.PathValue("name"), dst); err != nil {
		h.fail(w, err)
		return
	}
	inst, err := h.backend.GetInstance(r.Context(), dst)
	if err != nil {
		h.fail(w, err)
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.InstanceRow(h.backend.Capabilities(r.Context()), inst))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
