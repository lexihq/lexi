package server

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"

	"github.com/a-h/templ"
)

type handlers struct {
	backend backend.Backend
}

func (h handlers) list(w http.ResponseWriter, r *http.Request) {
	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.render(w, r, http.StatusOK, ui.InstancesPage(h.backend.Capabilities(), instances))
}

func (h handlers) detail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	snapshots, err := h.backend.ListSnapshots(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}

	h.render(w, r, http.StatusOK, ui.InstancePage(h.backend.Capabilities(), inst, snapshots))
}

func (h handlers) createForm(w http.ResponseWriter, r *http.Request) {
	images, err := h.backend.ListImages(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.render(w, r, http.StatusOK, ui.CreatePage(h.backend.Capabilities(), images))
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
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
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

func (h handlers) updateLimits(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	if err := h.backend.UpdateLimits(r.Context(), name, backend.Limits{
		CPU:    strings.TrimSpace(r.Form.Get("cpu")),
		Memory: strings.TrimSpace(r.Form.Get("memory")),
	}); err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.LimitsForm(inst))
		return
	}
	http.Redirect(w, r, "/instances/"+name, http.StatusSeeOther)
}

// metrics renders the self-refreshing live-metrics panel for an instance.
func (h handlers) metrics(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	m, err := h.backend.Metrics(r.Context(), name)
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	h.render(w, r, http.StatusOK, ui.MetricsPanel(name, m))
}

func (h handlers) start(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, func(name string) error { return h.backend.StartInstance(r.Context(), name) })
}

func (h handlers) stop(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, func(name string) error { return h.backend.StopInstance(r.Context(), name) })
}

func (h handlers) delete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.backend.DeleteInstance(r.Context(), name); err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	if isHTMX(r) {
		writeHTML(w, http.StatusOK)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h handlers) clone(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dst := strings.TrimSpace(r.Form.Get("dst"))
	if dst == "" {
		h.renderError(w, http.StatusBadRequest, "clone name is required")
		return
	}
	if err := h.backend.CloneInstance(r.Context(), r.PathValue("name"), dst); err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	inst, err := h.backend.GetInstance(r.Context(), dst)
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.InstanceRow(h.backend.Capabilities(), inst))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

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
	if err := h.backend.CreateSnapshot(r.Context(), name, snapshot); err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	h.renderSnapshotsOrRedirect(w, r, name)
}

func (h handlers) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.backend.RestoreSnapshot(r.Context(), name, r.PathValue("snap")); err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	h.renderSnapshotsOrRedirect(w, r, name)
}

func (h handlers) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.backend.DeleteSnapshot(r.Context(), name, r.PathValue("snap")); err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	h.renderSnapshotsOrRedirect(w, r, name)
}

func (h handlers) instanceAction(w http.ResponseWriter, r *http.Request, action func(string) error) {
	name := r.PathValue("name")
	if err := action(name); err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.InstanceRow(h.backend.Capabilities(), inst))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h handlers) renderSnapshotsOrRedirect(w http.ResponseWriter, r *http.Request, name string) {
	if !isHTMX(r) {
		http.Redirect(w, r, "/instances/"+name, http.StatusSeeOther)
		return
	}
	snapshots, err := h.backend.ListSnapshots(r.Context(), name)
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	h.render(w, r, http.StatusOK, ui.SnapshotTable(name, snapshots))
}

func (h handlers) renderError(w http.ResponseWriter, code int, message string) {
	writeHTML(w, code)
	_, _ = fmt.Fprintf(w, `<div role="alert">%s</div>`, html.EscapeString(message))
}

func (h handlers) render(w http.ResponseWriter, r *http.Request, code int, component templ.Component) {
	writeHTML(w, code)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func writeHTML(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, backend.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, backend.ErrConflict):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func esc(s string) string {
	return html.EscapeString(s)
}
