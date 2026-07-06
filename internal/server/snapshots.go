package server

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/ui"
)

// snapshotExpiryLayout is the absolute-time expiry format (the shape an
// <input type="datetime-local"> submits), still accepted alongside durations.
const snapshotExpiryLayout = "2006-01-02T15:04"

// expiryDurationRe matches the duration shorthand operators think in ("2w",
// "7d", "12h", "30m") — the same convention the snapshot schedule's expiry
// field uses.
var expiryDurationRe = regexp.MustCompile(`^(\d+)([mhdw])$`)

func (h handlers) createSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	// The UI hides snapshots on tiers without the capability; re-check here so
	// a crafted request is rejected too (same defense as the bulk endpoint).
	if !h.backend.Capabilities(r.Context()).Snapshots {
		h.fail(w, r, fmt.Errorf("snapshots are not supported here: %w", backend.ErrUnsupported))
		return
	}
	snapshot := strings.TrimSpace(r.Form.Get("snapshot"))
	if snapshot == "" {
		h.renderError(w, r, http.StatusBadRequest, "snapshot name is required")
		return
	}
	expiresAt, err := parseSnapshotExpiry(r.Form.Get("expires_at"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	opts := backend.SnapshotOptions{Stateful: r.Form.Get("stateful") != "", ExpiresAt: expiresAt}
	if err := h.backend.CreateSnapshot(r.Context(), name, snapshot, opts); err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderSnapshotsOrRedirect(w, r, name, "Snapshot created")
}

func (h handlers) renameSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	newName := strings.TrimSpace(r.Form.Get("new_name"))
	if newName == "" {
		h.fail(w, r, fmt.Errorf("new snapshot name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.RenameSnapshot(r.Context(), name, r.PathValue("snap"), newName); err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderSnapshotsOrRedirect(w, r, name, "Snapshot renamed")
}

func (h handlers) updateSnapshotExpiry(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	expiresAt, err := parseSnapshotExpiry(r.Form.Get("expires_at"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.backend.UpdateSnapshotExpiry(r.Context(), name, r.PathValue("snap"), expiresAt); err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderSnapshotsOrRedirect(w, r, name, "Expiry updated")
}

// snapshotSchedule renders the (lazily loaded) auto-snapshot schedule form.
func (h handlers) snapshotSchedule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s, err := h.backend.GetSnapshotSchedule(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.SnapshotScheduleForm(name, s))
}

// setSnapshotSchedule writes the three snapshots.* keys, then re-renders the
// schedule form on HTMX (else redirects to the instance).
func (h handlers) setSnapshotSchedule(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	s := backend.SnapshotSchedule{
		Schedule: strings.TrimSpace(r.Form.Get("schedule")),
		Expiry:   strings.TrimSpace(r.Form.Get("expiry")),
		Pattern:  strings.TrimSpace(r.Form.Get("pattern")),
	}
	if err := h.backend.SetSnapshotSchedule(r.Context(), name, s); err != nil {
		h.fail(w, r, err)
		return
	}
	if !isHTMX(r) {
		redirectToInstance(w, name)
		return
	}
	cur, err := h.backend.GetSnapshotSchedule(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.SnapshotScheduleForm(name, cur))
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
	// Operators think in relative terms ("keep for 2 weeks"), so durations are
	// the primary form; an absolute UTC time still works for exact deadlines.
	if m := expiryDurationRe.FindStringSubmatch(v); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid expiry %q: %w", v, backend.ErrInvalid)
		}
		unit := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}[m[2]]
		return time.Now().UTC().Add(time.Duration(n) * unit), nil
	}
	t, err := time.Parse(snapshotExpiryLayout, v) // no zone in layout → parsed as UTC
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid expiry %q (use 2w/7d/12h or YYYY-MM-DDTHH:MM): %w", v, backend.ErrInvalid)
	}
	return t, nil
}

func (h handlers) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.backend.RestoreSnapshot(r.Context(), name, r.PathValue("snap")); err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderSnapshotsOrRedirect(w, r, name, "Snapshot restored")
}

func (h handlers) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.backend.DeleteSnapshot(r.Context(), name, r.PathValue("snap")); err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderSnapshotsOrRedirect(w, r, name, "Snapshot deleted")
}

// renderSnapshotsOrRedirect re-renders the snapshot table on HTMX (else
// redirects), appending an out-of-band success toast carrying msg.
func (h handlers) renderSnapshotsOrRedirect(w http.ResponseWriter, r *http.Request, name, msg string) {
	if !isHTMX(r) {
		redirectToInstance(w, name)
		return
	}
	snapshots, err := h.backend.ListSnapshots(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderWithToast(w, r, http.StatusOK, ui.SnapshotTable(name, snapshots), msg)
}
