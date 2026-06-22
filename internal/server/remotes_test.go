package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cookieRequest issues a GET carrying the given cookies.
func cookieRequest(t *testing.T, srv *http.Server, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, strings.NewReader(""))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	return res
}

func TestRemoteCookieScopesRequests(t *testing.T) {
	b := fake.New()
	// The secondary remote starts without local's seeded instances; one
	// created there must be what the scoped request sees.
	secCtx := backend.WithRemote(context.Background(), "secondary")
	require.NoError(t, b.CreateInstance(secCtx, backend.CreateOptions{Name: "sec-inst", Image: "debian/12"}))

	res := cookieRequest(t, New(b), "/", &http.Cookie{Name: remoteCookie, Value: "secondary"}) //nolint:gosec // G124: request cookie; attributes are response-only.
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "sec-inst")
}

func TestStaleRemoteCookieExpiresAndFallsBack(t *testing.T) {
	res := cookieRequest(t, New(fake.New()), "/", &http.Cookie{Name: remoteCookie, Value: "ghost"}) //nolint:gosec // G124: request cookie; attributes are response-only.
	assertStatus(t, res, http.StatusOK)

	expired := false
	for _, c := range res.Result().Cookies() {
		if c.Name == remoteCookie && c.MaxAge < 0 {
			expired = true
		}
	}
	assert.True(t, expired, "stale remote cookie must be expired")
}

func TestSelectRemoteSetsCookieAndClearsProject(t *testing.T) {
	srv := New(fake.New())
	res := formRequest(t, srv, "/remote", url.Values{"remote": {"secondary"}}, false)
	assertStatus(t, res, http.StatusSeeOther)

	var gotRemote, clearedProject bool
	for _, c := range res.Result().Cookies() {
		if c.Name == remoteCookie && c.Value == "secondary" {
			gotRemote = true
		}
		if c.Name == projectCookie && c.MaxAge < 0 {
			clearedProject = true
		}
	}
	assert.True(t, gotRemote, "remote cookie must be set")
	assert.True(t, clearedProject, "project cookie must be cleared on remote switch")

	res = formRequest(t, srv, "/remote", url.Values{"remote": {"ghost"}}, false)
	assertStatus(t, res, http.StatusNotFound)
}

func TestMigrateInstanceHandler(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(context.Background(), backend.CreateOptions{Name: "mig-h", Image: "debian/12"}))
	srv := New(b)

	// Same-remote target is rejected: migration needs another daemon.
	res := formRequest(t, srv, "/instances/mig-h/migrate", url.Values{"target": {"local"}}, false)
	assertStatus(t, res, http.StatusBadRequest)

	// Missing target.
	res = formRequest(t, srv, "/instances/mig-h/migrate", url.Values{}, false)
	assertStatus(t, res, http.StatusBadRequest)

	// Happy path: redirect to the list; the instance now lives on secondary.
	res = formRequest(t, srv, "/instances/mig-h/migrate", url.Values{"target": {"secondary"}, "new_name": {"mig-h2"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	_, err := b.GetInstance(context.Background(), "mig-h")
	require.ErrorIs(t, err, backend.ErrNotFound)
	_, err = b.GetInstance(backend.WithRemote(context.Background(), "secondary"), "mig-h2")
	require.NoError(t, err)
}
