package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
)

// maxFileUploadBytes caps a file upload so push cannot exhaust memory (the
// incus driver buffers the content). A var (not const) so tests can lower it.
var maxFileUploadBytes int64 = 1 << 30 // 1 GiB

// requestPath extracts and validates the ?path= query parameter, defaulting to
// the filesystem root.
func requestPath(r *http.Request) (string, error) {
	p := r.URL.Query().Get("path")
	if p == "" {
		p = "/"
	}
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("path %q must be absolute: %w", p, backend.ErrInvalid)
	}
	return p, nil
}

// filesPanel renders the Files tab listing for an instance directory.
func (h handlers) filesPanel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dir, err := requestPath(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	entries, err := h.backend.ListFiles(r.Context(), name, dir)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.FilesPanel(name, dir, entries))
}

// attachmentWriter defers the download headers until the backend produces the
// first byte, so an error on open (ghost path, directory) can still render a
// clean error response instead of corrupting a started download.
type attachmentWriter struct {
	w        http.ResponseWriter
	filename string
	wrote    bool
}

func (a *attachmentWriter) Write(p []byte) (int, error) {
	if !a.wrote {
		a.setHeaders()
	}
	return a.w.Write(p)
}

func (a *attachmentWriter) setHeaders() {
	a.wrote = true
	a.w.Header().Set("Content-Type", "application/octet-stream")
	a.w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, a.filename))
}

// downloadFile streams an instance file as an attachment.
func (h handlers) downloadFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	filePath, err := requestPath(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	aw := &attachmentWriter{w: w, filename: path.Base(filePath)}
	if err := h.backend.PullFile(r.Context(), name, filePath, aw); err != nil {
		if aw.wrote {
			// Headers are gone; the download is already broken. Nothing clean
			// to send — surface it in the status text via a plain error.
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.fail(w, err)
		return
	}
	if !aw.wrote {
		aw.setHeaders() // empty file: still deliver a (zero-byte) download
	}
}

// uploadFile pushes a multipart upload into the instance directory given by
// the path field, then re-renders the panel at that directory.
func (h handlers) uploadFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	r.Body = http.MaxBytesReader(w, r.Body, maxFileUploadBytes)
	// The request body is bounded by MaxBytesReader immediately above.
	if err := r.ParseMultipartForm(32 << 20); err != nil { //nolint:gosec // G120: MaxBytesReader caps the complete upload.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.renderError(w, http.StatusRequestEntityTooLarge, "file is too large")
			return
		}
		h.renderError(w, http.StatusBadRequest, err.Error())
		return
	}
	dir := strings.TrimSpace(r.FormValue("path"))
	if !strings.HasPrefix(dir, "/") {
		h.renderError(w, http.StatusBadRequest, "directory path must be absolute")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer closeAndLog("uploaded file", file)

	// Some browsers send a client-side path; keep only the base name.
	base := path.Base(strings.ReplaceAll(header.Filename, `\`, "/"))
	if base == "." || base == "/" || base == "" {
		h.renderError(w, http.StatusBadRequest, "upload has no usable file name")
		return
	}
	target := strings.TrimSuffix(dir, "/") + "/" + base
	if err := h.backend.PushFile(r.Context(), name, target, file); err != nil {
		h.fail(w, err)
		return
	}
	if isHTMX(r) {
		entries, err := h.backend.ListFiles(r.Context(), name, dir)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.render(w, r, http.StatusOK, ui.FilesPanel(name, dir, entries))
		return
	}
	http.Redirect(w, r, "/instances/"+url.PathEscape(name)+"?tab=files", http.StatusSeeOther)
}
