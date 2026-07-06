package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/ui"
)

// volumeBackupsPanel is the swappable backups section on the volume page.
func (h handlers) volumeBackupsPanel(w http.ResponseWriter, r *http.Request) {
	pool, volume := r.PathValue("pool"), r.PathValue("volume")
	bks, err := h.backend.ListVolumeBackups(r.Context(), pool, volume)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	pools, err := h.backend.ListStoragePools(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.VolumeBackupsTable(h.backend.Capabilities(r.Context()), pool, volume, bks, pools))
}

// createVolumeBackup stores a server-side backup and re-renders the section.
func (h handlers) createVolumeBackup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pool, volume := r.PathValue("pool"), r.PathValue("volume")
	expiresAt, err := parseSnapshotExpiry(r.Form.Get("expires_at"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	backupName := strings.TrimSpace(r.Form.Get("name"))
	volumeOnly := r.Form.Get("volume_only") != ""
	if err := h.backend.CreateVolumeBackup(r.Context(), pool, volume, backupName, expiresAt, volumeOnly); err != nil {
		h.fail(w, r, err)
		return
	}
	h.redirectOrVolumeBackups(w, r, pool, volume)
}

func (h handlers) deleteVolumeBackup(w http.ResponseWriter, r *http.Request) {
	pool, volume := r.PathValue("pool"), r.PathValue("volume")
	if err := h.backend.DeleteVolumeBackup(r.Context(), pool, volume, r.PathValue("backup")); err != nil {
		h.fail(w, r, err)
		return
	}
	h.redirectOrVolumeBackups(w, r, pool, volume)
}

// exportVolumeBackup streams a stored backup's tarball. The attachmentWriter
// defers the download headers until the first byte, so a pre-stream failure
// renders a clean error and a rare mid-stream failure aborts without appending
// error text into the tarball.
func (h handlers) exportVolumeBackup(w http.ResponseWriter, r *http.Request) {
	pool, volume, backup := r.PathValue("pool"), r.PathValue("volume"), r.PathValue("backup")
	aw := &attachmentWriter{w: w, filename: volume + "-" + backup + ".tar.gz"}
	if err := h.backend.ExportVolumeBackup(r.Context(), pool, volume, backup, aw); err != nil {
		if aw.wrote {
			slog.Warn("volume backup download aborted mid-stream", "pool", pool, "volume", volume, "backup", backup, "err", err)
			return
		}
		h.fail(w, r, err)
		return
	}
	if !aw.wrote {
		aw.setHeaders() // empty tarball: still deliver a (zero-byte) download
	}
}

// restoreVolumeBackup creates a new volume in the chosen pool from a stored
// backup and lands on it.
func (h handlers) restoreVolumeBackup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	newName := strings.TrimSpace(r.Form.Get("name"))
	if newName == "" {
		h.fail(w, r, fmt.Errorf("new volume name is required: %w", backend.ErrInvalid))
		return
	}
	pool, volume := r.PathValue("pool"), r.PathValue("volume")
	targetPool := strings.TrimSpace(r.Form.Get("target_pool"))
	if targetPool == "" {
		targetPool = pool
	}
	if err := h.backend.RestoreVolumeBackup(r.Context(), pool, volume, r.PathValue("backup"), targetPool, newName); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/storage/"+url.PathEscape(targetPool)+"/volumes/"+url.PathEscape(newName), http.StatusSeeOther)
}

// redirectOrVolumeBackups re-renders the section for HTMX requests and
// redirects plain form posts back to the volume page.
func (h handlers) redirectOrVolumeBackups(w http.ResponseWriter, r *http.Request, pool, volume string) {
	if isHTMX(r) {
		h.volumeBackupsPanel(w, r)
		return
	}
	http.Redirect(w, r, "/storage/"+url.PathEscape(pool)+"/volumes/"+url.PathEscape(volume), http.StatusSeeOther)
}
