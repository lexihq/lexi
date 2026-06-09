package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
)

// snapshotExpiryLayout is the format an <input type="datetime-local"> submits.
const snapshotExpiryLayout = "2006-01-02T15:04"

func (h handlers) createSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	snapshot := strings.TrimSpace(r.Form.Get("snapshot"))
	if snapshot == "" {
		h.renderError(w, http.StatusBadRequest, "snapshot name is required")
		return
	}
	expiresAt, err := parseSnapshotExpiry(r.Form.Get("expires_at"))
	if err != nil {
		h.fail(w, err)
		return
	}
	opts := backend.SnapshotOptions{Stateful: r.Form.Get("stateful") != "", ExpiresAt: expiresAt}
	if err := h.backend.CreateSnapshot(r.Context(), name, snapshot, opts); err != nil {
		h.fail(w, err)
		return
	}
	h.renderSnapshotsOrRedirect(w, r, name)
}

func (h handlers) renameSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	newName := strings.TrimSpace(r.Form.Get("new_name"))
	if newName == "" {
		h.fail(w, fmt.Errorf("new snapshot name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.RenameSnapshot(r.Context(), name, r.PathValue("snap"), newName); err != nil {
		h.fail(w, err)
		return
	}
	h.renderSnapshotsOrRedirect(w, r, name)
}

func (h handlers) updateSnapshotExpiry(w http.ResponseWriter, r *http.Request) {
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
	if err := h.backend.UpdateSnapshotExpiry(r.Context(), name, r.PathValue("snap"), expiresAt); err != nil {
		h.fail(w, err)
		return
	}
	h.renderSnapshotsOrRedirect(w, r, name)
}

// parseSnapshotExpiry parses a datetime-local value as UTC; an empty value means
// "no expiry" (zero time). The <input type="datetime-local"> carries no timezone
// offset, so we fix the interpretation to UTC (and label the field UTC) rather
// than the server's local zone — otherwise the absolute expiry would shift with
// wherever the server happens to run.
func parseSnapshotExpiry(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(snapshotExpiryLayout, v) // no zone in layout → parsed as UTC
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid expiry %q: %w", v, backend.ErrInvalid)
	}
	return t, nil
}

func (h handlers) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.backend.RestoreSnapshot(r.Context(), name, r.PathValue("snap")); err != nil {
		h.fail(w, err)
		return
	}
	h.renderSnapshotsOrRedirect(w, r, name)
}

func (h handlers) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.backend.DeleteSnapshot(r.Context(), name, r.PathValue("snap")); err != nil {
		h.fail(w, err)
		return
	}
	h.renderSnapshotsOrRedirect(w, r, name)
}

func (h handlers) renderSnapshotsOrRedirect(w http.ResponseWriter, r *http.Request, name string) {
	if !isHTMX(r) {
		redirectToInstance(w, name)
		return
	}
	snapshots, err := h.backend.ListSnapshots(r.Context(), name)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.SnapshotTable(name, snapshots))
}
