package server

import (
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"

	"github.com/a-h/templ"
	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
)

type handlers struct {
	backend backend.Backend
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
