package server

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/lexihq/lexi/internal/ui"
)

// maxImportBytes caps an uploaded backup tarball so import cannot exhaust the
// temp filesystem. Generous enough for real instance backups, bounded enough to
// stop a runaway upload. A var (not const) so tests can lower it.
var maxImportBytes int64 = 8 << 30 // 8 GiB

// importForm previously served a dedicated import page; the upload form now
// lives in a header-button dialog on the instance list, so the old route just
// redirects there (keeps deep links / bookmarks from 404ing).
func (h handlers) importForm(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// importInstance restores an instance from an uploaded backup tarball. The file
// upload uses a plain multipart form, so success redirects to the list (and
// returns the new row when driven by HTMX, mirroring create).
func (h handlers) importInstance(w http.ResponseWriter, r *http.Request) {
	// Cap the whole request body so a large or malicious upload cannot spool an
	// unbounded tarball to the temp filesystem before import begins.
	if !h.parseMultipartUpload(w, r, maxImportBytes, "backup file is too large") {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.renderError(w, r, http.StatusBadRequest, "name is required")
		return
	}
	file, _, err := r.FormFile("backup")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "backup file is required")
		return
	}
	defer closeAndLog("uploaded backup file", file)

	if err := h.backend.ImportInstance(r.Context(), name, file); err != nil {
		h.fail(w, r, err)
		return
	}
	if isHTMX(r) {
		inst, err := h.backend.GetInstance(r.Context(), name)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		h.render(w, r, http.StatusOK, ui.InstanceRow(h.backend.Capabilities(r.Context()), inst))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// export streams a portable backup tarball as a file download. It validates the
// instance up front so a missing one returns a clean 404. The attachmentWriter
// defers the download headers until the first byte, so a pre-stream failure
// renders a clean error and a rare mid-stream failure aborts without appending
// error text into the tarball.
func (h handlers) export(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, err := h.backend.GetInstance(r.Context(), name); err != nil {
		h.fail(w, r, err)
		return
	}
	aw := &attachmentWriter{w: w, filename: name + ".tar.gz"}
	if err := h.backend.ExportInstance(r.Context(), name, aw); err != nil {
		if aw.wrote {
			slog.Warn("export aborted mid-stream", "instance", name, "err", err)
			return
		}
		h.fail(w, r, err)
		return
	}
	if !aw.wrote {
		aw.setHeaders() // empty tarball: still deliver a (zero-byte) download
	}
}
