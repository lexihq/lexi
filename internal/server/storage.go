package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
)

func (h handlers) storagePools(w http.ResponseWriter, r *http.Request) {
	pools, err := h.backend.ListStoragePools(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.StoragePoolsPage(h.backend.Capabilities(), pools))
}

func (h handlers) storagePool(w http.ResponseWriter, r *http.Request) {
	pool := r.PathValue("pool")
	p, err := h.backend.GetStoragePool(r.Context(), pool)
	if err != nil {
		h.fail(w, err)
		return
	}
	vols, err := h.backend.ListVolumes(r.Context(), pool)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.StoragePoolPage(h.backend.Capabilities(), p, vols))
}

// createVolume builds a custom volume from the form (name/content-type + optional
// key/value config rows) and redirects to the pool. Incus validates the config.
func (h handlers) createVolume(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pool := r.PathValue("pool")
	name := strings.TrimSpace(r.Form.Get("name"))
	contentType := strings.TrimSpace(r.Form.Get("content_type"))
	if name == "" || contentType == "" {
		h.fail(w, fmt.Errorf("volume name and content type are required: %w", backend.ErrInvalid))
		return
	}
	v := backend.StorageVolume{
		Name:        name,
		ContentType: contentType,
		Config:      zipConfigPairs(r.Form["key"], r.Form["value"]),
	}
	if err := h.backend.CreateVolume(r.Context(), pool, v); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/storage/"+pool, http.StatusSeeOther)
}

// deleteVolume removes a custom volume, then re-renders the volumes table on HTMX.
func (h handlers) deleteVolume(w http.ResponseWriter, r *http.Request) {
	pool := r.PathValue("pool")
	if err := h.backend.DeleteVolume(r.Context(), pool, r.PathValue("volume")); err != nil {
		h.fail(w, err)
		return
	}
	vols, err := h.backend.ListVolumes(r.Context(), pool)
	if err != nil {
		h.fail(w, err)
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.StorageVolumesTable(pool, vols))
		return
	}
	http.Redirect(w, r, "/storage/"+pool, http.StatusSeeOther)
}
