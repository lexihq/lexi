package server

import (
	"net/http"

	"github.com/adam/lxcon/internal/ui"
)

// operationsPanel renders the polled body of the bottom Tasks panel.
func (h handlers) operationsPanel(w http.ResponseWriter, r *http.Request) {
	ops, err := h.backend.ListOperations(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.OperationRows(ops))
}

// cancelOperation cancels a running operation, then re-renders the Tasks panel
// body so the status flips in place.
func (h handlers) cancelOperation(w http.ResponseWriter, r *http.Request) {
	if err := h.backend.CancelOperation(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	h.operationsPanel(w, r)
}
