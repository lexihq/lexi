package server

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/adam/lxcon/internal/backend"
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

	writeHTML(w, http.StatusOK)
	_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body><main><h1>Instances</h1><table><tbody>`)
	for _, inst := range instances {
		h.writeInstanceRow(w, inst)
	}
	_, _ = fmt.Fprint(w, `</tbody></table></main></body></html>`)
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

	writeHTML(w, http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html><body><main><h1>%s</h1><p>%s</p>`, esc(inst.Name), esc(inst.Status))
	h.writeSnapshotTable(w, inst.Name, snapshots)
	_, _ = fmt.Fprint(w, `</main></body></html>`)
}

func (h handlers) createForm(w http.ResponseWriter, r *http.Request) {
	images, err := h.backend.ListImages(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeHTML(w, http.StatusOK)
	_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body><main><form method="post" action="/instances"><input name="name"><select name="image">`)
	for _, img := range images {
		_, _ = fmt.Fprintf(w, `<option value="%s">%s</option>`, esc(img.Alias), esc(img.Description))
	}
	_, _ = fmt.Fprint(w, `</select><label><input type="checkbox" name="start"> Start</label><button type="submit">Create</button></form></main></body></html>`)
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

	if err := h.backend.CreateInstance(r.Context(), backend.CreateOptions{
		Name:  name,
		Image: image,
		Start: r.Form.Get("start") != "",
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
		writeHTML(w, http.StatusOK)
		h.writeInstanceRow(w, inst)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
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
		writeHTML(w, http.StatusOK)
		h.writeInstanceRow(w, inst)
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
		writeHTML(w, http.StatusOK)
		h.writeInstanceRow(w, inst)
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
	writeHTML(w, http.StatusOK)
	h.writeSnapshotTable(w, name, snapshots)
}

func (h handlers) writeInstanceRow(w http.ResponseWriter, inst backend.Instance) {
	_, _ = fmt.Fprintf(w, `<tr id="instance-%s"><td><a href="/instances/%s">%s</a></td><td>%s</td><td>%s</td><td>%d</td><td><button hx-post="/instances/%s/start" hx-target="#instance-%s" hx-swap="outerHTML">Start</button><button hx-post="/instances/%s/stop" hx-target="#instance-%s" hx-swap="outerHTML">Stop</button><button hx-post="/instances/%s/delete" hx-target="#instance-%s" hx-swap="outerHTML">Delete</button></td></tr>`, esc(inst.Name), esc(inst.Name), esc(inst.Name), esc(inst.Status), esc(strings.Join(inst.IPv4, ", ")), inst.Snapshots, esc(inst.Name), esc(inst.Name), esc(inst.Name), esc(inst.Name), esc(inst.Name), esc(inst.Name))
}

func (h handlers) writeSnapshotTable(w http.ResponseWriter, name string, snapshots []backend.Snapshot) {
	_, _ = fmt.Fprintf(w, `<section id="snapshots"><form method="post" action="/instances/%s/snapshots"><input name="snapshot"><button type="submit">Snapshot</button></form><table><tbody>`, esc(name))
	for _, snapshot := range snapshots {
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td><button hx-post="/instances/%s/snapshots/%s/restore" hx-target="#snapshots" hx-swap="outerHTML">Restore</button><button hx-post="/instances/%s/snapshots/%s/delete" hx-target="#snapshots" hx-swap="outerHTML">Delete</button></td></tr>`, esc(snapshot.Name), esc(name), esc(snapshot.Name), esc(name), esc(snapshot.Name))
	}
	_, _ = fmt.Fprint(w, `</tbody></table></section>`)
}

func (h handlers) renderError(w http.ResponseWriter, code int, message string) {
	writeHTML(w, code)
	_, _ = fmt.Fprintf(w, `<div role="alert">%s</div>`, esc(message))
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func writeHTML(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
}

func statusFor(err error) int {
	msg := err.Error()
	if strings.Contains(msg, "not found") {
		return http.StatusNotFound
	}
	if strings.Contains(msg, "already exists") {
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

func esc(s string) string {
	return html.EscapeString(s)
}
