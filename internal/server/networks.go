package server

import (
	"net/http"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
)

func (h handlers) networks(w http.ResponseWriter, r *http.Request) {
	nets, err := h.backend.ListNetworks(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.NetworksPage(h.backend.Capabilities(), nets))
}

func (h handlers) networkDetail(w http.ResponseWriter, r *http.Request) {
	n, err := h.backend.GetNetwork(r.Context(), r.PathValue("name"))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.NetworkDetailPage(h.backend.Capabilities(), n))
}

func (h handlers) networkCreateForm(w http.ResponseWriter, r *http.Request) {
	h.renderShell(w, r, http.StatusOK, ui.NetworkCreatePage(h.backend.Capabilities()))
}

// createNetwork builds a network from the form (name/type/description + optional
// key/value config rows) and redirects to the list. Incus validates the config.
func (h handlers) createNetwork(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	n := backend.Network{
		Name:        r.Form.Get("name"),
		Type:        r.Form.Get("type"),
		Description: r.Form.Get("description"),
		Config:      zipConfigPairs(r.Form["key"], r.Form["value"]),
	}
	if err := h.backend.CreateNetwork(r.Context(), n); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/networks", http.StatusSeeOther)
}

// deleteNetwork removes a network, then re-renders the list table on HTMX.
func (h handlers) deleteNetwork(w http.ResponseWriter, r *http.Request) {
	if err := h.backend.DeleteNetwork(r.Context(), r.PathValue("name")); err != nil {
		h.fail(w, err)
		return
	}
	nets, err := h.backend.ListNetworks(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.NetworksTable(nets))
		return
	}
	http.Redirect(w, r, "/networks", http.StatusSeeOther)
}
