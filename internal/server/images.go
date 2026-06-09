package server

import (
	"fmt"
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
		ui.ImagesPage(h.backend.Capabilities(), images, instances))
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
