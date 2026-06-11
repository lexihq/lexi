package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/adam/lxcon/internal/backend"
)

// remoteCookie persists the UI's remote selection across requests.
const remoteCookie = "lxcon-remote"

// withRemote tags every request context with the validated remote selection
// from the cookie. A stale cookie — the remote left the config or was down at
// startup — is expired and the request continues on the default remote, so
// the UI can never be trapped on an unreachable server. It runs before
// withProject, so project validation happens against the selected remote.
func (h handlers) withRemote(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.backend.Capabilities(r.Context()).Remotes {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(remoteCookie)
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		name, err := url.QueryUnescape(c.Value)
		if err != nil || name == "" {
			next.ServeHTTP(w, r)
			return
		}
		known, err := h.remoteKnown(r, name)
		if err != nil {
			http.Error(w, err.Error(), statusFor(err))
			return
		}
		if !known {
			expireRemoteCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(backend.WithRemote(r.Context(), name)))
	})
}

// remoteKnown reports whether the named remote is in the reachable set.
func (h handlers) remoteKnown(r *http.Request, name string) (bool, error) {
	remotes, err := h.backend.ListRemotes(r.Context())
	if err != nil {
		return false, err
	}
	for _, rem := range remotes {
		if rem.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// selectRemote switches the UI's remote: validate, set the cookie, and land
// on the instance list. The project cookie is cleared — project names don't
// transfer between daemons, and a stale selection would scope the first
// requests to a project the new remote may not have.
func (h handlers) selectRemote(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("remote"))
	if name == "" {
		h.fail(w, fmt.Errorf("remote name is required: %w", backend.ErrInvalid))
		return
	}
	known, err := h.remoteKnown(r, name)
	if err != nil {
		h.fail(w, err)
		return
	}
	if !known {
		h.fail(w, fmt.Errorf("remote %q: %w", name, backend.ErrNotFound))
		return
	}
	setRemoteCookie(w, name)
	expireProjectCookie(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// setRemoteCookie pins the remote selection; query-escaped and without the
// Secure attribute for the same reasons as the project cookie (see
// setProjectCookie).
func setRemoteCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: remoteCookie, Value: url.QueryEscape(name), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode}) //nolint:gosec // G124: see setProjectCookie.
}

func expireRemoteCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: remoteCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode}) //nolint:gosec // G124: expiry; see setProjectCookie.
}
