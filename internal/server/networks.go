package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
)

func (h handlers) networks(w http.ResponseWriter, r *http.Request) {
	nets, err := h.backend.ListNetworks(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.NetworksPage(h.backend.Capabilities(r.Context()), nets))
}

func (h handlers) networkDetail(w http.ResponseWriter, r *http.Request) {
	n, err := h.backend.GetNetwork(r.Context(), r.PathValue("name"))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.NetworkDetailPage(h.backend.Capabilities(r.Context()), n))
}

func (h handlers) networkCreateForm(w http.ResponseWriter, r *http.Request) {
	h.renderShell(w, r, http.StatusOK, ui.NetworkCreatePage(h.backend.Capabilities(r.Context())))
}

// createNetwork builds a network from the form (name/type/description + optional
// key/value config rows) and redirects to the list. Incus validates the config.
func (h handlers) createNetwork(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	netType := strings.TrimSpace(r.Form.Get("type"))
	if name == "" || netType == "" {
		h.fail(w, fmt.Errorf("network name and type are required: %w", backend.ErrInvalid))
		return
	}
	n := backend.Network{
		Name:        name,
		Type:        netType,
		Description: r.Form.Get("description"),
		Config:      zipConfigPairs(r.Form["key"], r.Form["value"]),
	}
	if err := h.backend.CreateNetwork(r.Context(), n); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/networks", http.StatusSeeOther)
}

// updateNetwork applies the network editor: description plus key/value rows
// that replace the network's config (instance-config-editor semantics). The
// hidden version field carries the token the form was rendered from, so a
// concurrent change conflicts (409) instead of being silently overwritten.
func (h handlers) updateNetwork(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	config := zipConfigPairs(r.Form["key"], r.Form["value"])
	if err := h.backend.UpdateNetwork(r.Context(), name, r.Form.Get("description"), config, r.Form.Get("version")); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/networks/"+url.PathEscape(name), http.StatusSeeOther)
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
