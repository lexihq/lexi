package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"

	"github.com/a-h/templ"
	"github.com/gorilla/websocket"
)

// maxImportBytes caps an uploaded backup tarball so import cannot exhaust the
// temp filesystem. Generous enough for real instance backups, bounded enough to
// stop a runaway upload. A var (not const) so tests can lower it.
var maxImportBytes int64 = 8 << 30 // 8 GiB

type handlers struct {
	backend backend.Backend
}

func (h handlers) list(w http.ResponseWriter, r *http.Request) {
	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// The list already has the instances the sidebar needs; reuse them.
	h.renderWithSidebar(w, r, http.StatusOK, instances, ui.InstancesPage(h.backend.Capabilities(), instances))
}

// sidebar renders the self-refreshing instance list for the shell sidebar. The
// active param (the currently-viewed instance name) drives the highlight.
func (h handlers) sidebar(w http.ResponseWriter, r *http.Request) {
	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.render(w, r, http.StatusOK, ui.SidebarInstances(instances, r.URL.Query().Get("active")))
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
	var profiles []backend.Profile
	if h.backend.Capabilities().Profiles {
		profiles, err = h.backend.ListProfiles(r.Context())
		if err != nil {
			http.Error(w, err.Error(), statusFor(err))
			return
		}
	}

	tab := r.URL.Query().Get("tab")
	// A tab click is an explicit (non-boosted) HTMX request and gets just the
	// swappable body. A boosted navigation (clicking the instance in the sidebar
	// or list) carries HX-Boosted and must get the full page so the shell's
	// #content swap finds the whole content region.
	if isHTMX(r) && !isBoosted(r) {
		h.render(w, r, http.StatusOK, ui.InstanceBody(h.backend.Capabilities(), inst, snapshots, profiles, tab))
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.InstancePage(h.backend.Capabilities(), inst, snapshots, profiles, tab))
}

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
	redirectToInstance(w, name)
}

func (h handlers) profiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := h.backend.ListProfiles(r.Context())
	if err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.ProfilesPage(h.backend.Capabilities(), profiles))
}

func (h handlers) profileDetail(w http.ResponseWriter, r *http.Request) {
	p, err := h.backend.GetProfile(r.Context(), r.PathValue("name"))
	if err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.ProfileDetailPage(h.backend.Capabilities(), p))
}

// setInstanceProfiles replaces the instance's profile set from the checked
// boxes, preserving existing order and appending additions (mergeProfileOrder),
// then returns the updated control on HTMX.
func (h handlers) setInstanceProfiles(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	ordered := mergeProfileOrder(inst.Profiles, r.Form["profile"])
	if err := h.backend.SetInstanceProfiles(r.Context(), name, ordered); err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	all, err := h.backend.ListProfiles(r.Context())
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	inst.Profiles = ordered
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.InstanceProfilesForm(inst, all))
		return
	}
	redirectToInstance(w, name)
}

// mergeProfileOrder keeps currently-assigned profiles that are still checked in
// their existing order, then appends newly-checked profiles in checked order. It
// dedupes so a doubled checkbox cannot duplicate an entry.
func mergeProfileOrder(current, checked []string) []string {
	inChecked := make(map[string]bool, len(checked))
	for _, c := range checked {
		inChecked[c] = true
	}
	out := make([]string, 0, len(checked))
	seen := make(map[string]bool, len(checked))
	for _, c := range current {
		if inChecked[c] && !seen[c] {
			out = append(out, c)
			seen[c] = true
		}
	}
	for _, c := range checked {
		if !seen[c] {
			out = append(out, c)
			seen[c] = true
		}
	}
	return out
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

// wsUpgrader upgrades console requests to WebSocket. The default same-origin
// check is sufficient: the terminal page is served from the same host.
var wsUpgrader = websocket.Upgrader{}

// console renders the full-page interactive terminal.
func (h handlers) console(w http.ResponseWriter, r *http.Request) {
	h.renderShell(w, r, http.StatusOK, ui.ConsolePage(h.backend.Capabilities(), r.PathValue("name")))
}

// consoleWS bridges a browser terminal to backend.Exec. Binary frames carry
// stdin/stdout bytes; text frames carry a {"cols","rows"} resize. A reader
// goroutine pumps client frames into the exec stdin pipe and resize channel
// while Exec writes stdout back as binary frames.
func (h handlers) consoleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote an error response.
	}
	defer closeAndLog("console WebSocket", conn)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	stdinR, stdinW := io.Pipe()
	resize := make(chan backend.WinSize, 1)

	go func() {
		defer cancel()
		defer closeAndLog("console stdin pipe", stdinW)
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			switch mt {
			case websocket.BinaryMessage:
				if _, err := stdinW.Write(data); err != nil {
					return
				}
			case websocket.TextMessage:
				var msg struct {
					Cols int `json:"cols"`
					Rows int `json:"rows"`
				}
				if err := json.Unmarshal(data, &msg); err != nil {
					continue
				}
				select {
				case resize <- backend.WinSize{Cols: msg.Cols, Rows: msg.Rows}:
				default: // drop a resize if one is already queued
				}
			}
		}
	}()

	err = h.backend.Exec(ctx, r.PathValue("name"), backend.ExecRequest{
		Stdin:  stdinR,
		Stdout: &wsConnWriter{conn: conn},
		Resize: resize,
	})
	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	if err != nil {
		closeMsg = websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())
	}
	if err := conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second)); err != nil {
		log.Printf("lxcon: write console WebSocket close message: %v", err)
	}
}

func closeAndLog(name string, closer io.Closer) {
	if err := closer.Close(); err != nil {
		log.Printf("lxcon: close %s: %v", name, err)
	}
}

// wsConnWriter adapts a WebSocket connection to io.Writer, sending each write as
// one binary frame. It is the sole writer to the connection during a session.
type wsConnWriter struct {
	conn *websocket.Conn
}

func (w *wsConnWriter) Write(p []byte) (int, error) {
	if err := w.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// logs renders the console-log panel for an instance.
func (h handlers) logs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	log, err := h.backend.ConsoleLog(r.Context(), name)
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	h.render(w, r, http.StatusOK, ui.LogsPanel(name, log))
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

func (h handlers) start(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, func(name string) error { return h.backend.StartInstance(r.Context(), name) })
}

func (h handlers) stop(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, func(name string) error { return h.backend.StopInstance(r.Context(), name) })
}

func (h handlers) restart(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, func(name string) error { return h.backend.RestartInstance(r.Context(), name) })
}

func (h handlers) pause(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, func(name string) error { return h.backend.PauseInstance(r.Context(), name) })
}

func (h handlers) resume(w http.ResponseWriter, r *http.Request) {
	h.instanceAction(w, r, func(name string) error { return h.backend.ResumeInstance(r.Context(), name) })
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
		redirectToInstance(w, name)
		return
	}
	snapshots, err := h.backend.ListSnapshots(r.Context(), name)
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	h.render(w, r, http.StatusOK, ui.SnapshotTable(name, snapshots))
}

func instanceURL(name string) string {
	return "/instances/" + url.PathEscape(name)
}

func redirectToInstance(w http.ResponseWriter, name string) {
	w.Header().Set("Location", instanceURL(name))
	w.WriteHeader(http.StatusSeeOther)
}

func (h handlers) renderError(w http.ResponseWriter, code int, message string) {
	writeHTML(w, code)
	if _, err := fmt.Fprintf(w, `<div role="alert">%s</div>`, html.EscapeString(message)); err != nil {
		log.Printf("lxcon: write error response: %v", err)
	}
}

func (h handlers) render(w http.ResponseWriter, r *http.Request, code int, component templ.Component) {
	writeHTML(w, code)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderShell renders a full-page component with the sidebar instance list
// preloaded, so the shell paints (and hx-boost re-swaps) with a populated
// sidebar instead of flashing an empty one. It fetches the list here (the
// sidebar is part of every full page); a list failure fails the page,
// consistent with h.list.
func (h handlers) renderShell(w http.ResponseWriter, r *http.Request, code int, component templ.Component) {
	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderWithSidebar(w, r, code, instances, component)
}

// renderWithSidebar renders a full-page component, injecting the given instance
// list into the context for the shell sidebar. Callers that already hold the
// list (e.g. the index page) use this directly to avoid a second fetch.
func (h handlers) renderWithSidebar(w http.ResponseWriter, r *http.Request, code int, instances []backend.Instance, component templ.Component) {
	writeHTML(w, code)
	ctx := ui.WithSidebarInstances(r.Context(), instances)
	if err := component.Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("Hx-Request") == "true"
}

// isBoosted reports whether the request came from an hx-boost navigation (as
// opposed to an explicit hx-get/hx-post partial). Boosted requests want the
// full page so the shell's content region swap has everything to select.
func isBoosted(r *http.Request) bool {
	return r.Header.Get("Hx-Boosted") == "true"
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
	case errors.Is(err, backend.ErrInvalid):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
