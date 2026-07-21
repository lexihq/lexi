package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/a-h/templ"
	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/metrics"
	"github.com/lexihq/lexi/internal/ui"
)

type handlers struct {
	backend backend.Backend
	samples *metrics.Store
}

func (h handlers) instanceAction(w http.ResponseWriter, r *http.Request, verb string, action func(string) error) {
	name := r.PathValue("name")
	if err := action(name); err != nil {
		h.fail(w, r, err)
		return
	}
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	// Affirm the action like every other mutation (config/devices/bulk): the
	// swapped fragment alone is easy to miss on slow lifecycle ops.
	msg := verb + " " + name
	if isHTMX(r) {
		// Detail-header buttons post with ?from=header and swap the header
		// fragment in place; list-row buttons swap their row.
		if r.URL.Query().Get("from") == "header" {
			h.renderWithToast(w, r, http.StatusOK, ui.InstanceHeader(h.backend.Capabilities(r.Context()), inst), msg)
			return
		}
		h.renderInstanceRow(w, r, inst, msg)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// renderInstanceRow renders a single swapped list row with the remote-switcher
// and CPU-trend context injected, so the row's Migrate… gating and sparkline
// survive the swap the same way they do on full table renders (the bug 208ccb2
// fixed for the table-fragment paths). A non-empty msg appends an out-of-band
// success toast.
func (h handlers) renderInstanceRow(w http.ResponseWriter, r *http.Request, inst backend.Instance, msg string) {
	ctx := ui.WithInstanceTrends(r.Context(), h.instanceTrends(r.Context(), []backend.Instance{inst}))
	// The project scope must survive row swaps too: destructive confirm
	// prompts name it via scopeSuffix (only the current name is needed here,
	// not the full switcher list).
	ctx = ui.WithProjectSwitcher(ctx, nil, backend.ProjectFromContext(r.Context()))
	r = r.WithContext(h.withRemoteSwitcher(ctx))
	h.renderWithToast(w, r, http.StatusOK, ui.InstanceRow(h.backend.Capabilities(r.Context()), inst), msg)
}

func instanceURL(name string) string {
	return "/instances/" + url.PathEscape(name)
}

func redirectToInstance(w http.ResponseWriter, name string) {
	w.Header().Set("Location", instanceURL(name))
	w.WriteHeader(http.StatusSeeOther)
}

// redirectToInstanceTab lands on one of the instance detail tabs. Like
// redirectToInstance, it writes the Location header directly (the path is
// escaped here, not attacker-shaped) to avoid http.Redirect's open-redirect
// taint on user-derived names.
func redirectToInstanceTab(w http.ResponseWriter, name, tab string) {
	w.Header().Set("Location", instanceURL(name)+"?tab="+tab)
	w.WriteHeader(http.StatusSeeOther)
}

// fail renders err as an HTMX error alert, mapping it to a status via statusFor.
// It's the common error path for handlers that act on a named instance.
func (h handlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.renderError(w, r, statusFor(err), err.Error())
}

// requireVersion returns the optimistic-concurrency token from the parsed
// form's hidden "version" field. A blank token would make the backend write
// unconditional — silently degrading to last-write-wins — so it fails the
// request with ErrInvalid instead; noun names the resource in the error. The
// bool reports whether the handler may proceed.
func (h handlers) requireVersion(w http.ResponseWriter, r *http.Request, noun string) (backend.Version, bool) {
	version := backend.Version(r.Form.Get("version"))
	if version == "" {
		h.fail(w, r, fmt.Errorf("missing %s version token: %w", noun, backend.ErrInvalid))
		return "", false
	}
	return version, true
}

func (h handlers) renderError(w http.ResponseWriter, r *http.Request, code int, message string) {
	// Native (non-HTMX) form posts — the hx-boost="false" dialogs — navigate to
	// the response, so a bare toast fragment would replace the whole page with
	// an unstyled div. Give them a full error page in the app shell instead.
	if !isHTMX(r) {
		h.renderErrorPage(w, r, code, message)
		return
	}
	writeHTML(w, code)
	if err := ui.ErrorToast(message).Render(context.Background(), w); err != nil {
		slog.Warn("write error response", "err", err)
	}
}

// renderErrorPage renders the styled full-page error. The sidebar instance
// list is best-effort — the error page must still render when the backend is
// the thing that's failing.
func (h handlers) renderErrorPage(w http.ResponseWriter, r *http.Request, code int, message string) {
	page := ui.ErrorPage(h.backend.Capabilities(r.Context()), code, message)
	instances, err := h.backend.ListInstances(r.Context())
	if err != nil {
		slog.Warn("error page sidebar", "err", err)
		h.render(w, r, code, page)
		return
	}
	h.renderWithSidebar(w, r, code, instances, page)
}

// parseMultipartUpload bounds the request body to limit and parses it as a
// multipart form. An over-limit body renders tooLargeMsg at 413; any other
// parse failure renders the error at 400. It returns false (after writing the
// response) when the caller should stop. Shared by the file-upload handlers so
// the body-cap policy lives in one place.
func (h handlers) parseMultipartUpload(w http.ResponseWriter, r *http.Request, limit int64, tooLargeMsg string) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	// The request body is bounded by MaxBytesReader immediately above.
	if err := r.ParseMultipartForm(32 << 20); err != nil { //nolint:gosec // G120: MaxBytesReader caps the complete upload.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.renderError(w, r, http.StatusRequestEntityTooLarge, tooLargeMsg)
			return false
		}
		h.renderError(w, r, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

// withRemoteSwitcher injects the remote list that the layout's remote switcher
// and InstanceRow's "Migrate…" gating read. A listing failure degrades to a
// hidden switcher (and no migrate targets) rather than failing the render.
// Shared by the full-page path and the table-fragment paths (the idle poll and
// bulk action), so the migrate affordance survives a partial re-render.
func (h handlers) withRemoteSwitcher(ctx context.Context) context.Context {
	if !h.backend.Capabilities(ctx).Remotes {
		return ctx
	}
	remotes, err := h.backend.ListRemotes(ctx)
	if err != nil {
		slog.Warn("list remotes for switcher", "err", err)
		return ctx
	}
	return ui.WithRemoteSwitcher(ctx, remotes)
}

func (h handlers) render(w http.ResponseWriter, r *http.Request, code int, component templ.Component) {
	writeHTML(w, code)
	if err := component.Render(r.Context(), w); err != nil {
		// Headers are already written, so no status can be set; typical cause
		// is the client aborting mid-render (e.g. navigating away).
		slog.Warn("render after headers", "err", err)
	}
}

// renderWithToast renders the success fragment and appends an out-of-band
// success toast so the action is affirmed without disturbing the swapped target.
// An empty msg renders the fragment alone, so callers with an optional toast
// don't each re-implement the branch. Callers gate this to HTMX requests (the
// non-HTMX path redirects instead).
func (h handlers) renderWithToast(w http.ResponseWriter, r *http.Request, code int, component templ.Component, msg string) {
	if msg == "" {
		h.render(w, r, code, component)
		return
	}
	writeHTML(w, code)
	if err := component.Render(r.Context(), w); err != nil {
		slog.Warn("render after headers", "err", err)
		return
	}
	if err := ui.SuccessToastOOB(msg).Render(r.Context(), w); err != nil {
		slog.Warn("render success toast", "err", err)
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
		h.render(w, r, statusFor(err), ui.ErrorPage(h.backend.Capabilities(r.Context()), statusFor(err), err.Error()))
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
	// The project switcher is layout-wide like the instance list; a listing
	// failure degrades to a hidden switcher rather than failing the page.
	if h.backend.Capabilities(r.Context()).Projects {
		if projects, err := h.backend.ListProjects(r.Context()); err == nil {
			ctx = ui.WithProjectSwitcher(ctx, projects, backend.ProjectFromContext(r.Context()))
		} else {
			slog.Warn("list projects for switcher", "err", err)
		}
	}
	// The remote switcher degrades the same way: hidden on a listing failure.
	ctx = h.withRemoteSwitcher(ctx)
	if err := component.Render(ctx, w); err != nil {
		// Headers are already written, so no status can be set; typical cause
		// is the client aborting mid-render (e.g. navigating away).
		slog.Warn("render after headers", "err", err)
	}
}

// csrfGuard rejects cross-site browser mutations: every state change in lexi
// is a POST, and trusting a pasted certificate or deleting a pool must not be
// triggerable by a foreign page's form. Browsers mark cross-site requests via
// Sec-Fetch-Site and send Origin on POSTs; requests carrying neither header
// (curl, Go tests) pass through — CSRF is a browser-only vector.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				http.Error(w, "cross-site request rejected", http.StatusForbidden)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					http.Error(w, "cross-origin request rejected", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isHTMX(r *http.Request) bool {
	// A history-restore request (Back/Forward with a cold htmx snapshot cache)
	// replays the URL expecting a full page to swap into document.body, so it
	// must get the non-HTMX (full page) response despite carrying Hx-Request.
	return r.Header.Get("Hx-Request") == "true" &&
		r.Header.Get("Hx-History-Restore-Request") != "true"
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
	case errors.Is(err, backend.ErrUnsupported):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}
