package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/ui"
)

// backupsPanel is the lazy-loaded Backups tab body.
func (h handlers) backupsPanel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	bks, err := h.backend.ListInstanceBackups(r.Context(), name)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.BackupsPanel(name, bks))
}

// createStoredBackup stores a server-side backup and re-renders the panel.
func (h handlers) createStoredBackup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	expiresAt, err := parseSnapshotExpiry(r.Form.Get("expires_at"))
	if err != nil {
		h.fail(w, err)
		return
	}
	backupName := strings.TrimSpace(r.Form.Get("name"))
	instanceOnly := r.Form.Get("instance_only") != ""
	if err := h.backend.CreateInstanceBackup(r.Context(), name, backupName, expiresAt, instanceOnly); err != nil {
		h.fail(w, err)
		return
	}
	h.redirectOrPanel(w, r, name)
}

func (h handlers) deleteStoredBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.backend.DeleteInstanceBackup(r.Context(), name, r.PathValue("backup")); err != nil {
		h.fail(w, err)
		return
	}
	h.redirectOrPanel(w, r, name)
}

// downloadStoredBackup streams a stored backup's tarball.
func (h handlers) downloadStoredBackup(w http.ResponseWriter, r *http.Request) {
	name, backup := r.PathValue("name"), r.PathValue("backup")
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, name+"-"+backup+".tar.gz"))
	if err := h.backend.ExportInstanceBackup(r.Context(), name, backup, w); err != nil {
		// Headers may already be written; the broken download is the signal.
		h.fail(w, err)
	}
}

// restoreStoredBackup creates a new instance from a stored backup and lands
// on it.
func (h handlers) restoreStoredBackup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	newName := strings.TrimSpace(r.Form.Get("new_name"))
	if newName == "" {
		h.fail(w, fmt.Errorf("new instance name is required: %w", backend.ErrInvalid))
		return
	}
	name := r.PathValue("name")
	if err := h.backend.RestoreInstanceBackup(r.Context(), name, r.PathValue("backup"), newName); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/instances/"+url.PathEscape(newName), http.StatusSeeOther)
}

// redirectOrPanel re-renders the Backups panel for HTMX requests and
// redirects plain form posts back to the tab.
func (h handlers) redirectOrPanel(w http.ResponseWriter, r *http.Request, name string) {
	if isHTMX(r) {
		h.backupsPanel(w, r)
		return
	}
	http.Redirect(w, r, "/instances/"+url.PathEscape(name)+"?tab=backups", http.StatusSeeOther)
}
