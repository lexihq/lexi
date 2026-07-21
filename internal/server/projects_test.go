package server

import (
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
	require.NoError(t, b.CreateProject(t.Context(), backend.Project{Name: "dev", Description: ""}))
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
	require.NoError(t, b.CreateProject(t.Context(), backend.Project{Name: "dev", Description: ""}))
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

func TestProjectsPageAndLifecycle(t *testing.T) {
	b := fake.New()
	srv := New(b)

	// Create with networks unchecked → explicit false; others checked.
	form := url.Values{"name": {"dev"}, "description": {"made by test"},
		"features.images": {"on"}, "features.profiles": {"on"}, "features.storage.volumes": {"on"}}
	res := projectRequest(t, srv, "POST", "/projects", form.Encode(), "")
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/projects/dev", res.Header().Get("Location"))

	p, err := b.GetProject(t.Context(), "dev")
	require.NoError(t, err)
	assert.Equal(t, "true", p.Config["features.profiles"])
	assert.Equal(t, "false", p.Config["features.networks"])

	// List page shows both projects and marks the current one.
	res = projectRequest(t, srv, "GET", "/projects", "", "dev")
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, "made by test")
	assert.Contains(t, body, "current")

	// Detail: versioned config update.
	res = projectRequest(t, srv, "POST", "/projects/dev/config",
		url.Values{"description": {"edited"}, "version": {string(p.Version)}, "key": {"features.profiles"}, "value": {"true"}}.Encode(), "")
	assertStatus(t, res, http.StatusSeeOther)
	p2, err := b.GetProject(t.Context(), "dev")
	require.NoError(t, err)
	assert.Equal(t, "edited", p2.Description)

	// Renaming the currently-selected project rewrites the cookie.
	res = projectRequest(t, srv, "POST", "/projects/dev/rename", url.Values{"new_name": {"dev2"}}.Encode(), "dev")
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/projects/dev2", res.Header().Get("Location"))
	cookies := res.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "dev2", cookies[0].Value)

	// Deleting the currently-selected project clears the cookie.
	res = projectRequest(t, srv, "POST", "/projects/dev2/delete", "", "dev2")
	assertStatus(t, res, http.StatusSeeOther)
	cookies = res.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Negative(t, cookies[0].MaxAge)
}

func TestProjectDetailGuardsDefault(t *testing.T) {
	res := projectRequest(t, New(fake.New()), "GET", "/projects/default", "", "")
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.NotContains(t, body, `action="/projects/default/rename"`, "default project must not offer rename")
	assert.NotContains(t, body, `action="/projects/default/delete"`, "default project must not offer delete")
}

// The daemon allows characters in project names that Go strips from raw
// cookie values (";" notably); selection must survive the escape round-trip.
func TestProjectCookieSurvivesSpecialCharacters(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateProject(t.Context(), backend.Project{Name: "a;b", Description: ""}))
	require.NoError(t, b.CreateInstance(backend.WithProject(t.Context(), "a;b"), backend.CreateOptions{Name: "odd-inst", Image: "alpine/edge"}))
	srv := New(b)

	res := projectRequest(t, srv, "POST", "/project", url.Values{"project": {"a;b"}}.Encode(), "")
	assertStatus(t, res, http.StatusSeeOther)
	cookies := res.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.NotContains(t, cookies[0].Value, ";", "the raw name would be truncated by cookie rules")

	// Replaying the cookie exactly as issued scopes to the right project.
	res = projectRequest(t, srv, "GET", "/", "", cookies[0].Value)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "odd-inst")
}

func TestProjectsPageShowsUsageInResourcesColumn(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "u1", Image: "debian/12"}))
	res := request(t, New(b), "GET", "/projects", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "1 instance")
}

func TestProjectDetailShowsUsageAndLimitsForm(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateProject(t.Context(), backend.Project{Name: "capped", Description: "", Config: map[string]string{
		"limits.instances": "5",
		"limits.memory":    "1GiB",
	}}))
	res := request(t, New(b), "GET", "/projects/capped", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, "Usage")
	assert.Contains(t, body, "instances")                        // usage row
	assert.Contains(t, body, "1.0 GiB")                          // parsed memory limit
	assert.Contains(t, body, `action="/projects/capped/limits"`) // limits form
	assert.Contains(t, body, `name="instances" value="5"`)       // prefilled from config
	assert.Contains(t, body, `name="memory" value="1GiB"`)
}

func TestUpdateProjectLimitsAppliesAndClears(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateProject(t.Context(), backend.Project{Name: "dev", Description: "", Config: map[string]string{"limits.cpu": "4"}}))

	res := formRequest(t, New(b), "/projects/dev/limits", url.Values{
		"instances": {"5"}, "memory": {"2GiB"}, "cpu": {""},
	}, false)
	assertStatus(t, res, http.StatusSeeOther)

	p, err := b.GetProject(t.Context(), "dev")
	require.NoError(t, err)
	assert.Equal(t, "5", p.Config["limits.instances"])
	assert.Equal(t, "2GiB", p.Config["limits.memory"])
	_, hasCPU := p.Config["limits.cpu"]
	assert.False(t, hasCPU, "empty field must clear the key")
}

func TestUpdateProjectLimitsRejectsBadValues(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateProject(t.Context(), backend.Project{Name: "dev", Description: ""}))

	res := formRequest(t, New(b), "/projects/dev/limits", url.Values{"instances": {"many"}}, false)
	assertStatus(t, res, http.StatusBadRequest)

	res = formRequest(t, New(b), "/projects/dev/limits", url.Values{"memory": {"10XB"}}, false)
	assertStatus(t, res, http.StatusBadRequest)

	res = formRequest(t, New(b), "/projects/dev/limits", url.Values{"instances": {"-2"}}, false)
	assertStatus(t, res, http.StatusBadRequest)

	// Nothing may have been applied.
	p, err := b.GetProject(t.Context(), "dev")
	require.NoError(t, err)
	for k := range p.Config {
		assert.False(t, strings.HasPrefix(k, "limits."), "no limits key may be set, got %q", k)
	}
}
