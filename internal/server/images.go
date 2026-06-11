package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
)

// imagesPage lists the local image store with copy/publish/alias controls. The
// instance list is fetched once and reused for both the sidebar and the
// publish form's instance select.
func (h handlers) imagesPage(w http.ResponseWriter, r *http.Request) {
	images, err := h.backend.ListLocalImages(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderWithSidebar(w, r, http.StatusOK, instances,
		ui.ImagesPage(h.backend.Capabilities(r.Context()), images, instances))
}

// imageAction runs a mutation, then re-renders the images table on HTMX or
// redirects back to the Images page.
func (h handlers) imageAction(w http.ResponseWriter, r *http.Request, action func() error) {
	if err := action(); err != nil {
		h.fail(w, err)
		return
	}
	if isHTMX(r) {
		images, err := h.backend.ListLocalImages(r.Context())
		if err != nil {
			h.fail(w, err)
			return
		}
		h.render(w, r, http.StatusOK, ui.ImagesTable(images))
		return
	}
	http.Redirect(w, r, "/images", http.StatusSeeOther)
}

func (h handlers) copyImage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	alias := strings.TrimSpace(r.Form.Get("alias"))
	if alias == "" {
		h.fail(w, fmt.Errorf("image alias is required: %w", backend.ErrInvalid))
		return
	}
	h.imageAction(w, r, func() error { return h.backend.CopyImage(r.Context(), alias) })
}

func (h handlers) publishImage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	instance := strings.TrimSpace(r.Form.Get("instance"))
	if instance == "" {
		h.fail(w, fmt.Errorf("instance is required: %w", backend.ErrInvalid))
		return
	}
	alias := strings.TrimSpace(r.Form.Get("alias"))
	h.imageAction(w, r, func() error { return h.backend.PublishImage(r.Context(), instance, alias) })
}

func (h handlers) deleteImage(w http.ResponseWriter, r *http.Request) {
	fingerprint := r.PathValue("fingerprint")
	h.imageAction(w, r, func() error { return h.backend.DeleteImage(r.Context(), fingerprint) })
}

// updateImage applies the per-image details form: description and the public
// visibility flag.
func (h handlers) updateImage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fingerprint := r.PathValue("fingerprint")
	description := strings.TrimSpace(r.Form.Get("description"))
	public := r.Form.Get("public") != ""
	h.imageAction(w, r, func() error { return h.backend.UpdateImage(r.Context(), fingerprint, description, public) })
}

// exportImage streams an image as a file download: a tarball for unified
// images (named by the driver with its real compression extension), a
// metadata+rootfs zip for split (VM) images. The driver spools the whole
// download before returning, so every failure — including a ghost
// fingerprint — arrives before any header or body byte is committed.
func (h handlers) exportImage(w http.ResponseWriter, r *http.Request) {
	fingerprint := r.PathValue("fingerprint")
	filename, rc, err := h.backend.ExportImage(r.Context(), fingerprint)
	if err != nil {
		h.fail(w, err)
		return
	}
	defer closeAndLog("image export spool", rc)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	if _, err := io.Copy(w, rc); err != nil {
		// Headers are committed; a copy failure means the client went away.
		slog.Warn("stream image export", "image", fingerprint, "err", err)
	}
}

// importImage creates a local image from an uploaded unified tarball. The file
// upload uses a plain multipart form, so success redirects to the Images page.
func (h handlers) importImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
	// The request body is bounded by MaxBytesReader immediately above.
	if err := r.ParseMultipartForm(32 << 20); err != nil { //nolint:gosec // G120: MaxBytesReader caps the complete upload.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.renderError(w, http.StatusRequestEntityTooLarge, "image file is too large")
			return
		}
		h.renderError(w, http.StatusBadRequest, err.Error())
		return
	}
	file, _, err := r.FormFile("image")
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "image file is required")
		return
	}
	defer closeAndLog("uploaded image file", file)

	if err := h.backend.ImportImage(r.Context(), file, strings.TrimSpace(r.FormValue("alias"))); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/images", http.StatusSeeOther)
}

func (h handlers) addImageAlias(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	alias := strings.TrimSpace(r.Form.Get("alias"))
	if alias == "" {
		h.fail(w, fmt.Errorf("image alias is required: %w", backend.ErrInvalid))
		return
	}
	fingerprint := r.PathValue("fingerprint")
	h.imageAction(w, r, func() error { return h.backend.AddImageAlias(r.Context(), fingerprint, alias) })
}

// removeImageAlias takes the alias in the form body (not the path) because
// aliases routinely contain slashes ("debian/12"), which path segments can't
// carry.
func (h handlers) removeImageAlias(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	alias := strings.TrimSpace(r.Form.Get("alias"))
	if alias == "" {
		h.fail(w, fmt.Errorf("image alias is required: %w", backend.ErrInvalid))
		return
	}
	h.imageAction(w, r, func() error { return h.backend.RemoveImageAlias(r.Context(), alias) })
}
