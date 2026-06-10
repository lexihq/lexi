package server

import (
	"fmt"
	"net/http"
	"net/url"
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

func (h handlers) poolCreateForm(w http.ResponseWriter, r *http.Request) {
	h.renderShell(w, r, http.StatusOK, ui.StoragePoolCreatePage(h.backend.Capabilities()))
}

// createPool builds a pool from the form (name/driver/description + optional
// key/value config rows) and redirects to the list. Incus validates the config.
func (h handlers) createPool(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	driver := strings.TrimSpace(r.Form.Get("driver"))
	if name == "" || driver == "" {
		h.fail(w, fmt.Errorf("pool name and driver are required: %w", backend.ErrInvalid))
		return
	}
	p := backend.StoragePool{
		Name:        name,
		Driver:      driver,
		Description: r.Form.Get("description"),
		Config:      zipConfigPairs(r.Form["key"], r.Form["value"]),
	}
	if err := h.backend.CreateStoragePool(r.Context(), p); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/storage", http.StatusSeeOther)
}

// deletePool removes an unused pool from its detail page, then redirects to
// the list (the detail page no longer exists). In-use pools 409 in the backend.
// updatePool applies the pool editor: description plus key/value rows that
// replace the pool's config. The hidden version field carries the token the
// form was rendered from, so a concurrent change conflicts (409) instead of
// being silently overwritten.
func (h handlers) updatePool(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pool := r.PathValue("pool")
	config := zipConfigPairs(r.Form["key"], r.Form["value"])
	if err := h.backend.UpdateStoragePool(r.Context(), pool, r.Form.Get("description"), config, r.Form.Get("version")); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/storage/"+url.PathEscape(pool), http.StatusSeeOther)
}

func (h handlers) deletePool(w http.ResponseWriter, r *http.Request) {
	if err := h.backend.DeleteStoragePool(r.Context(), r.PathValue("pool")); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/storage", http.StatusSeeOther)
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
	h.renderVolumesOrRedirect(w, r, pool)
}

// deleteVolume removes a custom volume, then re-renders the volumes table on HTMX.
func (h handlers) deleteVolume(w http.ResponseWriter, r *http.Request) {
	pool := r.PathValue("pool")
	if err := h.backend.DeleteVolume(r.Context(), pool, r.PathValue("volume")); err != nil {
		h.fail(w, err)
		return
	}
	h.renderVolumesOrRedirect(w, r, pool)
}

// renderVolumesOrRedirect re-renders the swappable volumes table on HTMX (so the
// inline create/delete forms swap #volumes in place), else redirects to the pool.
func (h handlers) renderVolumesOrRedirect(w http.ResponseWriter, r *http.Request, pool string) {
	if !isHTMX(r) {
		// Set Location directly (with the pool escaped) rather than http.Redirect,
		// mirroring redirectToInstance — avoids an open-redirect on tainted input.
		w.Header().Set("Location", "/storage/"+url.PathEscape(pool))
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	vols, err := h.backend.ListVolumes(r.Context(), pool)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.StorageVolumesTable(pool, vols))
}

func (h handlers) storageVolume(w http.ResponseWriter, r *http.Request) {
	pool := r.PathValue("pool")
	volume := r.PathValue("volume")
	v, err := h.backend.GetVolume(r.Context(), pool, volume)
	if err != nil {
		h.fail(w, err)
		return
	}
	snaps, err := h.backend.ListVolumeSnapshots(r.Context(), pool, volume)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.StorageVolumePage(h.backend.Capabilities(), v, snaps))
}

func (h handlers) createVolumeSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pool := r.PathValue("pool")
	volume := r.PathValue("volume")
	snapshot := strings.TrimSpace(r.Form.Get("snapshot"))
	if snapshot == "" {
		h.fail(w, fmt.Errorf("snapshot name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.CreateVolumeSnapshot(r.Context(), pool, volume, snapshot); err != nil {
		h.fail(w, err)
		return
	}
	h.renderVolumeSnapshotsOrRedirect(w, r, pool, volume)
}

func (h handlers) restoreVolumeSnapshot(w http.ResponseWriter, r *http.Request) {
	pool := r.PathValue("pool")
	volume := r.PathValue("volume")
	if err := h.backend.RestoreVolumeSnapshot(r.Context(), pool, volume, r.PathValue("snap")); err != nil {
		h.fail(w, err)
		return
	}
	h.renderVolumeSnapshotsOrRedirect(w, r, pool, volume)
}

func (h handlers) deleteVolumeSnapshot(w http.ResponseWriter, r *http.Request) {
	pool := r.PathValue("pool")
	volume := r.PathValue("volume")
	if err := h.backend.DeleteVolumeSnapshot(r.Context(), pool, volume, r.PathValue("snap")); err != nil {
		h.fail(w, err)
		return
	}
	h.renderVolumeSnapshotsOrRedirect(w, r, pool, volume)
}

// updateVolume applies the volume editor: description plus key/value rows that
// replace the volume's config (resize = the "size" key). The hidden version
// field makes the write conditional (409 on a concurrent change).
func (h handlers) updateVolume(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pool := r.PathValue("pool")
	volume := r.PathValue("volume")
	config := zipConfigPairs(r.Form["key"], r.Form["value"])
	if err := h.backend.UpdateVolume(r.Context(), pool, volume, r.Form.Get("description"), config, r.Form.Get("version")); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/storage/"+url.PathEscape(pool)+"/volumes/"+url.PathEscape(volume), http.StatusSeeOther)
}

// renameVolume renames a custom volume and redirects to its new detail page.
func (h handlers) renameVolume(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pool := r.PathValue("pool")
	newName := strings.TrimSpace(r.Form.Get("new_name"))
	if newName == "" {
		h.fail(w, fmt.Errorf("new volume name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.RenameVolume(r.Context(), pool, r.PathValue("volume"), newName); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/storage/"+url.PathEscape(pool)+"/volumes/"+url.PathEscape(newName), http.StatusSeeOther)
}

func (h handlers) renameVolumeSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pool := r.PathValue("pool")
	volume := r.PathValue("volume")
	newName := strings.TrimSpace(r.Form.Get("new_name"))
	if newName == "" {
		h.fail(w, fmt.Errorf("new snapshot name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.RenameVolumeSnapshot(r.Context(), pool, volume, r.PathValue("snap"), newName); err != nil {
		h.fail(w, err)
		return
	}
	h.renderVolumeSnapshotsOrRedirect(w, r, pool, volume)
}

func (h handlers) updateVolumeSnapshotExpiry(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pool := r.PathValue("pool")
	volume := r.PathValue("volume")
	expiresAt, err := parseSnapshotExpiry(r.Form.Get("expires_at"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.backend.UpdateVolumeSnapshotExpiry(r.Context(), pool, volume, r.PathValue("snap"), expiresAt); err != nil {
		h.fail(w, err)
		return
	}
	h.renderVolumeSnapshotsOrRedirect(w, r, pool, volume)
}

// renderVolumeSnapshotsOrRedirect re-renders the swappable snapshots table on
// HTMX (so the inline forms swap #volume-snapshots in place), else redirects to
// the volume.
func (h handlers) renderVolumeSnapshotsOrRedirect(w http.ResponseWriter, r *http.Request, pool, volume string) {
	if !isHTMX(r) {
		w.Header().Set("Location", "/storage/"+url.PathEscape(pool)+"/volumes/"+url.PathEscape(volume))
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	snaps, err := h.backend.ListVolumeSnapshots(r.Context(), pool, volume)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.StorageVolumeSnapshotsTable(pool, volume, snaps))
}
