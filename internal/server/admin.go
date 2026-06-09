package server

import (
	"net/http"

	"github.com/adam/lxcon/internal/ui"
)

// serverPage renders the Server section: overview, config editor, certificate
// trust list, and warnings.
func (h handlers) serverPage(w http.ResponseWriter, r *http.Request) {
	overview, err := h.backend.GetServerOverview(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	config, err := h.backend.GetServerConfig(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	certs, err := h.backend.ListCertificates(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	warnings, err := h.backend.ListWarnings(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderShell(w, r, http.StatusOK,
		ui.ServerPage(h.backend.Capabilities(), overview, config, certs, warnings))
}

// updateServerConfig replaces the server config from the submitted key/value
// rows (instance-config-editor semantics: a removed row removes the key).
func (h handlers) updateServerConfig(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	config := zipConfigPairs(r.Form["key"], r.Form["value"])
	if err := h.backend.UpdateServerConfig(r.Context(), config); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/server", http.StatusSeeOther)
}

// deleteWarning removes a warning, then re-renders the warnings table on HTMX.
func (h handlers) deleteWarning(w http.ResponseWriter, r *http.Request) {
	if err := h.backend.DeleteWarning(r.Context(), r.PathValue("uuid")); err != nil {
		h.fail(w, err)
		return
	}
	if isHTMX(r) {
		warnings, err := h.backend.ListWarnings(r.Context())
		if err != nil {
			h.fail(w, err)
			return
		}
		h.render(w, r, http.StatusOK, ui.WarningsTable(warnings))
		return
	}
	http.Redirect(w, r, "/server", http.StatusSeeOther)
}
