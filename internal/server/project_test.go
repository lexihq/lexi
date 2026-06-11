package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// projectRequest issues a request carrying the project-selection cookie.
func projectRequest(t *testing.T, srv *http.Server, method, path, body, project string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if project != "" {
		req.AddCookie(&http.Cookie{Name: projectCookie, Value: project}) //nolint:gosec // G124: request cookie; attributes are response-only.
	}
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	return res
}

func TestProjectCookieScopesRequests(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateProject(t.Context(), "dev", "", nil))
	require.NoError(t, b.CreateInstance(backend.WithProject(t.Context(), "dev"), backend.CreateOptions{Name: "dev-only", Image: "alpine/edge"}))
	srv := New(b)

	// Scoped request sees the project's instance; unscoped does not.
	res := projectRequest(t, srv, "GET", "/", "", "dev")
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "dev-only")

	res = projectRequest(t, srv, "GET", "/", "", "")
	assertStatus(t, res, http.StatusOK)
	assert.NotContains(t, res.Body.String(), "dev-only")

	// The switcher renders both projects with the selection marked.
	res = projectRequest(t, srv, "GET", "/", "", "dev")
	assert.Contains(t, res.Body.String(), `value="dev" selected`)
}

func TestStaleProjectCookieFallsBackToDefault(t *testing.T) {
	b := fake.New()
	srv := New(b)

	// A stale selection must not trap the UI.
	res := projectRequest(t, srv, "GET", "/", "", "ghost")
	assertStatus(t, res, http.StatusOK)
	// The cookie is expired so later requests don't re-validate a ghost.
	cookies := res.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, projectCookie, cookies[0].Name)
	assert.Negative(t, cookies[0].MaxAge)
}

func TestSelectProjectSetsCookie(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateProject(t.Context(), "dev", "", nil))
	srv := New(b)

	res := projectRequest(t, srv, "POST", "/project", url.Values{"project": {"dev"}}.Encode(), "")
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/", res.Header().Get("Location"))
	cookies := res.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "dev", cookies[0].Value)

	// Selecting default clears the cookie instead of pinning it.
	res = projectRequest(t, srv, "POST", "/project", url.Values{"project": {"default"}}.Encode(), "dev")
	assertStatus(t, res, http.StatusSeeOther)
	cookies = res.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Negative(t, cookies[0].MaxAge)

	// Ghost projects are refused.
	res = projectRequest(t, srv, "POST", "/project", url.Values{"project": {"ghost"}}.Encode(), "")
	assertStatus(t, res, http.StatusNotFound)
}
