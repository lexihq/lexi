package server

import (
	"context"
	"errors"
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

func (h handlers) instanceAction(w http.ResponseWriter, r *http.Request, action func(string) error) {
	name := r.PathValue("name")
	if err := action(name); err != nil {
		h.fail(w, err)
		return
	}
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		h.fail(w, err)
		return
	}
	if isHTMX(r) {
		// Detail-header buttons post with ?from=header and swap the header
		// fragment in place; list-row buttons swap their row.
		if r.URL.Query().Get("from") == "header" {
			h.render(w, r, http.StatusOK, ui.InstanceHeader(h.backend.Capabilities(r.Context()), inst))
			return
		}
		h.render(w, r, http.StatusOK, ui.InstanceRow(h.backend.Capabilities(r.Context()), inst))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func instanceURL(name string) string {
	return "/instances/" + url.PathEscape(name)
}

func redirectToInstance(w http.ResponseWriter, name string) {
	w.Header().Set("Location", instanceURL(name))
	w.WriteHeader(http.StatusSeeOther)
}

// fail renders err as an HTMX error alert, mapping it to a status via statusFor.
// It's the common error path for handlers that act on a named instance.
func (h handlers) fail(w http.ResponseWriter, err error) {
	h.renderError(w, statusFor(err), err.Error())
}

func (h handlers) renderError(w http.ResponseWriter, code int, message string) {
	writeHTML(w, code)
	if err := ui.ErrorToast(message).Render(context.Background(), w); err != nil {
		slog.Warn("write error response", "err", err)
	}
}

func (h handlers) render(w http.ResponseWriter, r *http.Request, code int, component templ.Component) {
	writeHTML(w, code)
	if err := component.Render(r.Context(), w); err != nil {
		// Headers are already written, so no status can be set; typical cause
		// is the client aborting mid-render (e.g. navigating away).
		slog.Warn("render after headers", "err", err)
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
	if h.backend.Capabilities(r.Context()).Remotes {
		if remotes, err := h.backend.ListRemotes(r.Context()); err == nil {
			ctx = ui.WithRemoteSwitcher(ctx, remotes)
		} else {
			slog.Warn("list remotes for switcher", "err", err)
		}
	}
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
	case errors.Is(err, backend.ErrUnsupported):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}
