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

// A stale config-version token must conflict instead of silently overwriting a
// concurrent editor's save (same contract as the devices form).
func TestUpdateConfigStaleVersionConflicts(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	res := formRequest(t, New(b), "/instances/demo/config",
		url.Values{"key": {"security.nesting"}, "value": {"true"}, "version": {"999"}}, true)
	assertStatus(t, res, http.StatusConflict)

	// And a post without any token is rejected rather than written unconditionally.
	res = formRequest(t, New(b), "/instances/demo/config",
		url.Values{"key": {"security.nesting"}, "value": {"true"}}, true)
	assertStatus(t, res, http.StatusBadRequest)
}

// The single-row swap after a lifecycle action must keep the remote-switcher
// context, so Migrate… doesn't vanish from the row that just became eligible
// (the row-swap sibling of the 208ccb2 table-fragment fix).
func TestInstanceActionRowKeepsMigrateAction(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))
	// Materialize a second remote so Migrate has a target.
	secCtx := backend.WithRemote(context.Background(), "secondary")
	require.NoError(t, b.CreateInstance(secCtx, backend.CreateOptions{Name: "sec-inst", Image: "debian/12"}))
	require.NoError(t, b.StartInstance(t.Context(), "demo"))

	res := request(t, New(b), http.MethodPost, "/instances/demo/stop", "", true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "Migrate", "swapped row must keep the Migrate action")
}

// An htmx history restore (Back/Forward with a cold snapshot cache) carries
// HX-Request but expects a full page to swap into document.body — a bare tab
// fragment would strip the shell.
func TestHistoryRestoreGetsFullPage(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/instances/demo?tab=metrics", nil)
	req.Header.Set("Hx-Request", "true")
	req.Header.Set("Hx-History-Restore-Request", "true")
	res := httptest.NewRecorder()
	New(b).Handler.ServeHTTP(res, req)

	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "<html", "history restore must receive the full page, not a fragment")
}

// Errors on native (non-HTMX) form posts must be a plain error response, not a
// bare toast fragment masquerading as a page.
func TestRenameConflictNonHTMXGetsPlainError(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "a", Image: "debian/12"}))
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "b", Image: "debian/12"}))

	res := formRequest(t, New(b), "/instances/a/rename", url.Values{"new_name": {"b"}}, false)
	assertStatus(t, res, http.StatusConflict)
	assert.NotContains(t, res.Body.String(), "data-tui-toast", "non-HTMX error must not be a toast fragment")
}

// Unknown paths must 404 instead of rendering the instances page.
func TestUnknownPathIs404(t *testing.T) {
	res := request(t, New(fake.New()), http.MethodGet, "/no-such-page", "", false)
	assertStatus(t, res, http.StatusNotFound)
}

// A cookie naming the default remote is the same scope as no cookie: the page
// renders normally (no redirect), keeping metrics series keys aligned with the
// unscoped background sampler.
func TestDefaultRemoteCookieIsNormalized(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))
	srv := New(b)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", strings.NewReader(""))
	req.AddCookie(&http.Cookie{Name: remoteCookie, Value: "local"}) //nolint:gosec // G124: request cookie; attributes are response-only.
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "demo")
}
