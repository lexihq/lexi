package server

import (
	"net/http"

	"github.com/adam/lxcon/internal/ui"
)

// metrics renders the self-refreshing live-metrics panel for an instance.
func (h handlers) metrics(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	m, err := h.backend.Metrics(r.Context(), name)
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	h.render(w, r, http.StatusOK, ui.MetricsPanel(name, m))
}
