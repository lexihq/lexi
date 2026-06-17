package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
)

func (h handlers) renameInstance(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	newName := strings.TrimSpace(r.Form.Get("new_name"))
	if newName == "" {
		h.fail(w, fmt.Errorf("new instance name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.RenameInstance(r.Context(), r.PathValue("name"), newName); err != nil {
		h.fail(w, err)
		return
	}
	redirectToInstance(w, newName)
}

func (h handlers) moveInstance(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pool := strings.TrimSpace(r.Form.Get("pool"))
	if pool == "" {
		h.fail(w, fmt.Errorf("target pool is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.MoveInstance(r.Context(), r.PathValue("name"), pool); err != nil {
		h.fail(w, err)
		return
	}
	redirectToInstance(w, r.PathValue("name"))
}
