package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/ui"
)

// createForm previously served a dedicated create page; the create form now
// lives in a header-button dialog on the instance list, so the old route just
// redirects there (keeps deep links / bookmarks from 404ing).
func (h handlers) createForm(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// createDialogData loads the image catalog and the optional profile/pool/network
// selectors the create-instance dialog renders. Each listing is capability-gated
// and best-effort: a failure drops just that selector rather than failing the
// whole instance list the dialog is mounted on.
func (h handlers) createDialogData(ctx context.Context, caps backend.Capabilities) (images []backend.Image, profiles []backend.Profile, pools []backend.StoragePool, networks []backend.Network) {
	if got, err := h.backend.ListImages(ctx); err == nil {
		images = got
	} else {
		slog.Warn("list images for create dialog", "err", err)
	}
	if caps.Profiles {
		if got, err := h.backend.ListProfiles(ctx); err == nil {
			profiles = got
		} else {
			slog.Warn("list profiles for create dialog", "err", err)
		}
	}
	if caps.Storage {
		if got, err := h.backend.ListStoragePools(ctx); err == nil {
			pools = got
		} else {
			slog.Warn("list storage pools for create dialog", "err", err)
		}
	}
	if caps.Networks {
		if got, err := h.backend.ListNetworks(ctx); err == nil {
			for _, n := range got {
				if n.Managed {
					networks = append(networks, n)
				}
			}
		} else {
			slog.Warn("list networks for create dialog", "err", err)
		}
	}
	return
}

// imagePicker renders the HTMX-driven image search results for the create
// form, filtered by the q/arch/type query params over the backend's full
// catalog.
func (h handlers) imagePicker(w http.ResponseWriter, r *http.Request) {
	all, err := h.backend.ListImages(r.Context())
	if err != nil {
		h.fail(w, err)
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
		if typ != "" && string(img.Type) != typ {
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
		h.fail(w, err)
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
		// Optional overrides; all empty for a plain create.
		Profiles: r.Form["profile"],
		Pool:     strings.TrimSpace(r.Form.Get("pool")),
		Network:  strings.TrimSpace(r.Form.Get("network")),
		Config:   nilIfEmpty(zipConfigPairs(r.Form["key"], r.Form["value"])),
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
		h.render(w, r, http.StatusOK, ui.InstanceRow(h.backend.Capabilities(r.Context()), inst))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// rebuildForm renders the rebuild page for an existing instance: the create
// form's image picker over the same catalog, posting back to rebuild.
func (h handlers) rebuildForm(w http.ResponseWriter, r *http.Request) {
	inst, err := h.backend.GetInstance(r.Context(), r.PathValue("name"))
	if err != nil {
		h.fail(w, err)
		return
	}
	images, err := h.backend.ListImages(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.RebuildPage(h.backend.Capabilities(r.Context()), inst, images))
}

// rebuild reinstalls the instance from the selected catalog image and lands
// back on the instance page. Like create, the posted image is a fingerprint
// resolved against the catalog so the driver gets both alias and identity.
func (h handlers) rebuild(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	image := strings.TrimSpace(r.Form.Get("image"))
	if image == "" {
		h.renderError(w, http.StatusBadRequest, "image is required")
		return
	}
	images, err := h.backend.ListImages(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	selected, ok := imageByFingerprint(images, image)
	if !ok {
		h.renderError(w, http.StatusBadRequest, "selected image is unavailable")
		return
	}
	name := r.PathValue("name")
	if err := h.backend.RebuildInstance(r.Context(), name, selected.Alias, selected.Fingerprint); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/instances/"+url.PathEscape(name), http.StatusSeeOther)
}

// nilIfEmpty keeps CreateOptions.Config nil for a plain create so the zero
// value round-trips exactly like the pre-wizard form.
func nilIfEmpty(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}

func imageByFingerprint(images []backend.Image, fingerprint string) (backend.Image, bool) {
	for _, image := range images {
		if image.Fingerprint == fingerprint {
			return image, true
		}
	}
	return backend.Image{}, false
}
