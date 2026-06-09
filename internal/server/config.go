package server

import (
	"net/http"
	"strings"

	"github.com/adam/lxcon/internal/ui"
)

// config renders the lazy-loaded Configuration tab: the key/value editor over
// the instance's editable config plus a read-only device list.
func (h handlers) config(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cfg, err := h.backend.GetInstanceConfig(r.Context(), name)
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	h.render(w, r, http.StatusOK, ui.ConfigPanel(name, cfg))
}

// updateConfig replaces the instance's editable config from the parallel
// key/value form fields (whole-map replace; blank keys are dropped), then
// re-renders the panel.
func (h handlers) updateConfig(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	if err := h.backend.UpdateInstanceConfig(r.Context(), name, zipConfigPairs(r.Form["key"], r.Form["value"])); err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	cfg, err := h.backend.GetInstanceConfig(r.Context(), name)
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.ConfigPanel(name, cfg))
		return
	}
	redirectToInstance(w, name)
}

// zipConfigPairs pairs parallel key/value form fields into a map, dropping
// entries with a blank (trimmed) key. A missing value maps to "".
func zipConfigPairs(keys, values []string) map[string]string {
	out := make(map[string]string, len(keys))
	for i, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		v := ""
		if i < len(values) {
			v = values[i]
		}
		out[k] = v
	}
	return out
}
