package server

import (
	"net/http"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
)

func (h handlers) createForm(w http.ResponseWriter, r *http.Request) {
	images, err := h.backend.ListImages(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.renderShell(w, r, http.StatusOK, ui.CreatePage(h.backend.Capabilities(), images))
}

// images renders the HTMX-driven image search results, filtered by the q/arch/
// type query params over the backend's full catalog.
func (h handlers) images(w http.ResponseWriter, r *http.Request) {
	all, err := h.backend.ListImages(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	arch := strings.TrimSpace(r.URL.Query().Get("arch"))
	typ := strings.TrimSpace(r.URL.Query().Get("type"))
	h.render(w, r, http.StatusOK, ui.ImageResults(filterImages(all, q, arch, typ)))
}

func filterImages(images []backend.Image, q, arch, typ string) []backend.Image {
	out := make([]backend.Image, 0, len(images))
	for _, img := range images {
		if arch != "" && img.Arch != arch {
			continue
		}
		if typ != "" && img.Type != typ {
			continue
		}
		if q != "" && !imageMatchesQuery(img, q) {
			continue
		}
		out = append(out, img)
	}
	return out
}

// imageMatchesQuery reports whether q (already lower-cased) is a substring of
// any searchable image field.
func imageMatchesQuery(img backend.Image, q string) bool {
	for _, field := range []string{img.Alias, img.Description, img.Distribution, img.Release} {
		if strings.Contains(strings.ToLower(field), q) {
			return true
		}
	}
	return false
}

func (h handlers) create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	image := strings.TrimSpace(r.Form.Get("image"))
	if name == "" {
		h.renderError(w, http.StatusBadRequest, "name is required")
		return
	}
	if image == "" {
		h.renderError(w, http.StatusBadRequest, "image is required")
		return
	}

	images, err := h.backend.ListImages(r.Context())
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, err.Error())
		return
	}
	selected, ok := imageByFingerprint(images, image)
	if !ok {
		h.renderError(w, http.StatusBadRequest, "selected image is unavailable")
		return
	}

	if err := h.backend.CreateInstance(r.Context(), backend.CreateOptions{
		Name:        name,
		Image:       selected.Alias,
		Fingerprint: selected.Fingerprint,
		Type:        selected.Type,
		Start:       r.Form.Get("start") != "",
	}); err != nil {
		h.fail(w, err)
		return
	}
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		h.fail(w, err)
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.InstanceRow(h.backend.Capabilities(), inst))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func imageByFingerprint(images []backend.Image, fingerprint string) (backend.Image, bool) {
	for _, image := range images {
		if image.Fingerprint == fingerprint {
			return image, true
		}
	}
	return backend.Image{}, false
}
