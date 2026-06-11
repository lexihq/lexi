package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/adam/lxcon/internal/backend"
)

// projectCookie persists the UI's project selection across requests.
const projectCookie = "lxcon-project"

// withProject tags every request context with the validated project selection
// from the cookie. A stale cookie — the project was deleted or renamed since
// selection — is expired and the request continues under the default project,
// so the UI can never be trapped in a nonexistent project.
func (h handlers) withProject(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.backend.Capabilities(r.Context()).Projects {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(projectCookie)
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		name, err := url.QueryUnescape(c.Value)
		if err != nil || name == "" || name == "default" {
			next.ServeHTTP(w, r)
			return
		}
		if _, err := h.backend.GetProject(r.Context(), name); err != nil {
			if errors.Is(err, backend.ErrNotFound) {
				expireProjectCookie(w)
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, err.Error(), statusFor(err))
			return
		}
		next.ServeHTTP(w, r.WithContext(backend.WithProject(r.Context(), name)))
	})
}

// selectProject switches the UI's project: validate, set the cookie, and land
// on the instance list (the previous page's resource may not exist in the new
// project).
func (h handlers) selectProject(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("project"))
	if name == "" {
		h.fail(w, fmt.Errorf("project name is required: %w", backend.ErrInvalid))
		return
	}
	if name == "default" {
		expireProjectCookie(w)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if _, err := h.backend.GetProject(r.Context(), name); err != nil {
		h.fail(w, err)
		return
	}
	setProjectCookie(w, name)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// setProjectCookie pins the project selection. The name is query-escaped:
// the daemon allows characters in project names (e.g. ";") that Go silently
// strips from cookie values, which could otherwise scope requests to a
// different existing project. No Secure attribute: lxcon routinely serves
// plain HTTP (dev, LAN), where a Secure cookie silently breaks selection;
// the value is a non-secret UI preference.
func setProjectCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: projectCookie, Value: url.QueryEscape(name), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode}) //nolint:gosec // G124: see above.
}

func expireProjectCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: projectCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode}) //nolint:gosec // G124: expiry; see selectProject.
}
