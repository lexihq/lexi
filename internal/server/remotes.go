package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
)

// remoteCookie persists the UI's remote selection across requests.
const remoteCookie = "lexi-remote"

// withRemote tags every request context with the validated remote selection
// from the cookie. A stale cookie — the remote left the config or was down at
// startup — is expired and the request is bounced (redirect for GETs, 409 for
// mutations) rather than silently retargeted at the default remote, where a
// same-named instance could receive the action. A cookie naming the default
// remote leaves the context untagged, so scoping (e.g. metrics series keys,
// which the background sampler writes unscoped) matches the no-cookie state —
// the same normalization withProject applies to "default". It runs before
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
		known, isDefault, err := h.remoteKnown(r, name)
		if err != nil {
			http.Error(w, err.Error(), statusFor(err))
			return
		}
		if !known {
			expireRemoteCookie(w)
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				// Same-URL retry (now cookieless). Location is written
				// directly: RequestURI is always a rooted path+query, so
				// http.Redirect's open-redirect taint doesn't apply.
				w.Header().Set("Location", r.URL.RequestURI())
				w.WriteHeader(http.StatusSeeOther)
				return
			}
			h.renderError(w, r, http.StatusConflict, fmt.Sprintf("remote %q is no longer available; select a remote and retry", name))
			return
		}
		if isDefault {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(backend.WithRemote(r.Context(), name)))
	})
}

// remoteKnown reports whether the named remote is in the reachable set, and
// whether it is the request context's Current remote — which, on the untagged
// context withRemote runs with, is the default remote.
func (h handlers) remoteKnown(r *http.Request, name string) (known, isDefault bool, err error) {
	remotes, err := h.backend.ListRemotes(r.Context())
	if err != nil {
		return false, false, err
	}
	for _, rem := range remotes {
		if rem.Name == name {
			return true, rem.Current, nil
		}
	}
	return false, false, nil
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
		h.fail(w, r, fmt.Errorf("remote name is required: %w", backend.ErrInvalid))
		return
	}
	known, _, err := h.remoteKnown(r, name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if !known {
		h.fail(w, r, fmt.Errorf("remote %q: %w", name, backend.ErrNotFound))
		return
	}
	setRemoteCookie(w, name)
	expireProjectCookie(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// migrateInstance moves a stopped instance to another remote and lands on
// the instance list — the instance no longer exists on this daemon. The
// target must differ from the request's remote: a same-remote "migration" is
// a rename, which has its own action.
func (h handlers) migrateInstance(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	target := strings.TrimSpace(r.Form.Get("target"))
	if target == "" {
		h.fail(w, r, fmt.Errorf("target remote is required: %w", backend.ErrInvalid))
		return
	}
	current, err := h.currentRemote(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if target == current {
		h.fail(w, r, fmt.Errorf("instance is already on %q: %w", target, backend.ErrInvalid))
		return
	}
	name := r.PathValue("name")
	if err := h.backend.MigrateInstance(r.Context(), name, target, strings.TrimSpace(r.Form.Get("new_name"))); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// currentRemote names the remote the request is scoped to (the ListRemotes
// entry marked Current honors the default-remote fallback).
func (h handlers) currentRemote(r *http.Request) (string, error) {
	remotes, err := h.backend.ListRemotes(r.Context())
	if err != nil {
		return "", err
	}
	for _, rem := range remotes {
		if rem.Current {
			return rem.Name, nil
		}
	}
	return "", nil
}

// setRemoteCookie pins the remote selection (see setSelectionCookie for the
// escaping and attribute rationale).
func setRemoteCookie(w http.ResponseWriter, name string) { setSelectionCookie(w, remoteCookie, name) }

func expireRemoteCookie(w http.ResponseWriter) { expireSelectionCookie(w, remoteCookie) }
