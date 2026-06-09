package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/adam/lxcon/internal/ui"
)

// maxImportBytes caps an uploaded backup tarball so import cannot exhaust the
// temp filesystem. Generous enough for real instance backups, bounded enough to
// stop a runaway upload. A var (not const) so tests can lower it.
var maxImportBytes int64 = 8 << 30 // 8 GiB

// importForm renders the backup-upload page.
func (h handlers) importForm(w http.ResponseWriter, r *http.Request) {
	h.renderShell(w, r, http.StatusOK, ui.ImportPage(h.backend.Capabilities()))
}

// importInstance restores an instance from an uploaded backup tarball. The file
// upload uses a plain multipart form, so success redirects to the list (and
// returns the new row when driven by HTMX, mirroring create).
func (h handlers) importInstance(w http.ResponseWriter, r *http.Request) {
	// Cap the whole request body so a large or malicious upload cannot spool an
	// unbounded tarball to the temp filesystem before import begins.
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
	// The request body is bounded by MaxBytesReader immediately above.
	if err := r.ParseMultipartForm(32 << 20); err != nil { //nolint:gosec // G120: MaxBytesReader caps the complete upload.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.renderError(w, http.StatusRequestEntityTooLarge, "backup file is too large")
			return
		}
		h.renderError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.renderError(w, http.StatusBadRequest, "name is required")
		return
	}
	file, _, err := r.FormFile("backup")
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "backup file is required")
		return
	}
	defer closeAndLog("uploaded backup file", file)

	if err := h.backend.ImportInstance(r.Context(), name, file); err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	if isHTMX(r) {
		inst, err := h.backend.GetInstance(r.Context(), name)
		if err != nil {
			h.renderError(w, statusFor(err), err.Error())
			return
		}
		h.render(w, r, http.StatusOK, ui.InstanceRow(h.backend.Capabilities(), inst))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// export streams a portable backup tarball as a file download. It validates the
// instance up front so a missing one returns a clean 404 before any backup work
// or response body is committed.
func (h handlers) export(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, err := h.backend.GetInstance(r.Context(), name); err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, name+".tar.gz"))
	if err := h.backend.ExportInstance(r.Context(), name, w); err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
}
