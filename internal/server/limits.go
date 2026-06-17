package server

import (
	"net/http"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/ui"
)

func (h handlers) updateLimits(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	if err := h.backend.UpdateLimits(r.Context(), name, backend.Limits{
		CPU:    strings.TrimSpace(r.Form.Get("cpu")),
		Memory: strings.TrimSpace(r.Form.Get("memory")),
	}); err != nil {
		h.fail(w, err)
		return
	}
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		h.fail(w, err)
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.LimitsForm(inst))
		return
	}
	redirectToInstance(w, name)
}
