package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/adam/lxcon/internal/backend"
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
	config, configVersion, err := h.backend.GetServerConfig(r.Context())
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
		ui.ServerPage(h.backend.Capabilities(), overview, config, configVersion, certs, warnings))
}

// updateServerConfig replaces the server config from the submitted key/value
// rows (instance-config-editor semantics: a removed row removes the key). The
// hidden version field carries the config version the form was rendered from,
// so a concurrent change conflicts (409) instead of being silently overwritten.
func (h handlers) updateServerConfig(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	config := zipConfigPairs(r.Form["key"], r.Form["value"])
	if err := h.backend.UpdateServerConfig(r.Context(), config, r.Form.Get("version")); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/server", http.StatusSeeOther)
}

// addCertificate adds a pasted PEM certificate to the trust store, then
// redirects to the Server page.
func (h handlers) addCertificate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	certType := r.Form.Get("type")
	pemData := strings.TrimSpace(r.Form.Get("certificate"))
	if name == "" || pemData == "" {
		h.fail(w, fmt.Errorf("certificate name and PEM data are required: %w", backend.ErrInvalid))
		return
	}
	if certType != "client" && certType != "metrics" {
		h.fail(w, fmt.Errorf("certificate type %q must be client or metrics: %w", certType, backend.ErrInvalid))
		return
	}
	if err := h.backend.AddCertificate(r.Context(), name, certType, pemData); err != nil {
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
