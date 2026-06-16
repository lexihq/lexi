package server

import (
	"net/http"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
)

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

	// The list already has the instances the sidebar needs; reuse them.
	h.renderWithSidebar(w, r, http.StatusOK, instances, ui.InstancesPage(caps, instances, images, profiles, pools, networks))
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
