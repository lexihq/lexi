package server

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
)

// maxFileUploadBytes caps a file upload so push cannot exhaust memory (the
// incus driver buffers the content). A var (not const) so tests can lower it.
var maxFileUploadBytes int64 = 1 << 30 // 1 GiB

// requestPath extracts and validates the ?path= query parameter, defaulting to
// the filesystem root.
func requestPath(r *http.Request) (string, error) {
	return normalizeAbsPath(r.URL.Query().Get("path"))
}

// normalizeAbsPath requires an absolute path and canonicalizes it (dot
// segments, doubled and trailing slashes) so every driver sees the same form.
func normalizeAbsPath(p string) (string, error) {
	if p == "" {
		p = "/"
	}
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("path %q must be absolute: %w", p, backend.ErrInvalid)
	}
	return path.Clean(p), nil
}

// filesPanel renders the Files tab listing for an instance directory.
func (h handlers) filesPanel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dir, err := requestPath(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderFiles(w, r, name, dir)
}

// renderFiles re-renders the Files panel at dir for HTMX requests, otherwise
// redirects back to the instance's Files tab.
func (h handlers) renderFiles(w http.ResponseWriter, r *http.Request, name, dir string) {
	if !isHTMX(r) {
		http.Redirect(w, r, "/instances/"+url.PathEscape(name)+"?tab=files", http.StatusSeeOther)
		return
	}
	entries, err := h.backend.ListFiles(r.Context(), name, dir)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.FilesPanel(h.backend.Capabilities(), name, dir, entries))
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
			// The 200 and part of the body are already sent; writing anything
			// more would corrupt the download with error text. Log and abort.
			slog.Warn("download aborted mid-stream", "instance", name, "path", filePath, "err", err)
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
	dir, err := normalizeAbsPath(strings.TrimSpace(r.FormValue("path")))
	if err != nil {
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
	if err := h.backend.PushFile(r.Context(), name, target, file, backend.FileWriteOptions{}); err != nil {
		h.fail(w, err)
		return
	}
	h.renderFiles(w, r, name, dir)
}

// deleteFile removes the instance file (or empty directory) given by the path
// form value, then re-renders the panel at its parent directory.
func (h handlers) deleteFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := normalizeAbsPath(strings.TrimSpace(r.FormValue("path")))
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.backend.DeleteFile(r.Context(), name, p); err != nil {
		h.fail(w, err)
		return
	}
	h.renderFiles(w, r, name, path.Dir(p))
}

// maxEditableFileBytes caps what the in-browser editor will load (inclusive);
// larger files must be downloaded instead.
const maxEditableFileBytes = 1 << 20 // 1 MiB

// maxViewableFileBytes caps what the read-only viewer loads; a larger file is
// shown truncated with a download prompt. A var (not const) so tests can lower it.
var maxViewableFileBytes int64 = 2 << 20 // 2 MiB

// editFileForm renders the in-browser editor for a text file: its content in
// a textarea plus the ownership and mode captured at read time, which the save
// posts back so the write preserves them.
func (h handlers) editFileForm(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := requestPath(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	var buf bytes.Buffer
	info, err := h.backend.PullFileInfo(r.Context(), name, p, &buf, maxEditableFileBytes)
	if err != nil {
		h.fail(w, err)
		return
	}
	if info.Type != "file" {
		h.renderError(w, http.StatusBadRequest, fmt.Sprintf("%q is not an editable file", p))
		return
	}
	content := buf.Bytes()
	if bytes.ContainsRune(content, 0) || !utf8.Valid(content) {
		h.renderError(w, http.StatusBadRequest, fmt.Sprintf("%q is a binary file; download it instead", p))
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.FileEditorPage(h.backend.Capabilities(), name, p, string(content), info))
}

// viewFile renders a read-only view of a file: its content (truncated to
// maxViewableFileBytes) in a <pre>, tolerant of large or binary logs the editor
// refuses. Non-UTF8 bytes are replaced so the page stays valid UTF-8.
func (h handlers) viewFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := requestPath(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	var buf bytes.Buffer
	info, truncated, err := h.backend.PullFileHead(r.Context(), name, p, &buf, maxViewableFileBytes)
	if err != nil {
		h.fail(w, err)
		return
	}
	if info.Type != "file" {
		h.renderError(w, http.StatusBadRequest, fmt.Sprintf("%q is not a viewable file", p))
		return
	}
	// Neutralize bytes that break the page: invalid UTF-8 sequences and NUL
	// (valid UTF-8, but the editor's binary marker and unsafe to render raw).
	content := strings.ToValidUTF8(buf.String(), "�")
	content = strings.ReplaceAll(content, "\x00", "�")
	h.renderShell(w, r, http.StatusOK, ui.FileViewerPage(h.backend.Capabilities(), name, p, content, info, truncated))
}

// saveFile writes the edited content back with the ownership and mode the
// editor captured at read time, then redirects to the Files tab. It enforces
// the same bounds as the read path: size-capped, text-only content.
func (h handlers) saveFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := requestPath(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	// 4x leaves room for the URL-encoding of the form body (worst case 3x)
	// plus the metadata fields; the decoded content is checked exactly below.
	r.Body = http.MaxBytesReader(w, r.Body, 4*maxEditableFileBytes)
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.renderError(w, http.StatusRequestEntityTooLarge, "file is too large to edit")
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	uid, err := strconv.ParseInt(r.Form.Get("uid"), 10, 64)
	if err != nil || uid < 0 {
		h.fail(w, fmt.Errorf("bad uid %q: %w", r.Form.Get("uid"), backend.ErrInvalid))
		return
	}
	gid, err := strconv.ParseInt(r.Form.Get("gid"), 10, 64)
	if err != nil || gid < 0 {
		h.fail(w, fmt.Errorf("bad gid %q: %w", r.Form.Get("gid"), backend.ErrInvalid))
		return
	}
	if mode, err := strconv.ParseUint(r.Form.Get("mode"), 8, 32); err != nil || mode > 0o777 {
		h.fail(w, fmt.Errorf("bad mode %q: %w", r.Form.Get("mode"), backend.ErrInvalid))
		return
	}
	// Textareas submit CRLF line endings; instance files are LF.
	content := strings.ReplaceAll(r.Form.Get("content"), "\r\n", "\n")
	if int64(len(content)) > maxEditableFileBytes {
		h.renderError(w, http.StatusRequestEntityTooLarge, "file is too large to edit")
		return
	}
	if strings.ContainsRune(content, 0) || !utf8.ValidString(content) {
		h.renderError(w, http.StatusBadRequest, "content must be text")
		return
	}
	opts := backend.FileWriteOptions{Mode: r.Form.Get("mode"), UID: uid, GID: gid}
	if err := h.backend.PushFile(r.Context(), name, p, strings.NewReader(content), opts); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/instances/"+url.PathEscape(name)+"?tab=files", http.StatusSeeOther)
}

// makeDirectory creates a folder named by the name form value inside the dir
// form value, then re-renders the panel at dir.
func (h handlers) makeDirectory(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dir, err := normalizeAbsPath(strings.TrimSpace(r.FormValue("dir")))
	if err != nil {
		h.fail(w, err)
		return
	}
	folder := strings.TrimSpace(r.FormValue("name"))
	if folder == "" || folder == "." || folder == ".." || strings.Contains(folder, "/") {
		h.fail(w, fmt.Errorf("folder name %q must be a single path component: %w", folder, backend.ErrInvalid))
		return
	}
	if err := h.backend.MakeDirectory(r.Context(), name, strings.TrimSuffix(dir, "/")+"/"+folder); err != nil {
		h.fail(w, err)
		return
	}
	h.renderFiles(w, r, name, dir)
}
