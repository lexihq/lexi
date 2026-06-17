package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstanceURLPathEscapesName(t *testing.T) {
	assert.Equal(t, "/instances/demo", instanceURL("demo"))
	assert.Equal(t, "/instances/%2F%2Fevil.example", instanceURL("//evil.example"))
}

func TestStatusForSentinels(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	// Missing instance → 404.
	missing := request(t, New(b), "GET", "/instances/ghost", "", true)
	assert.Equal(t, http.StatusNotFound, missing.Code)

	// Duplicate create → 409.
	dup := formRequest(t, New(b), "/instances", url.Values{"name": {"demo"}, "image": {"fake-debian-12-aarch64"}}, true)
	assert.Equal(t, http.StatusConflict, dup.Code)

	assert.Equal(t, http.StatusBadRequest, statusFor(fmt.Errorf("invalid limits: %w", backend.ErrInvalid)))
	assert.Equal(t, http.StatusUnprocessableEntity, statusFor(fmt.Errorf("split image: %w", backend.ErrUnsupported)))
}

func TestCSRFGuardBlocksCrossOriginPost(t *testing.T) {
	srv := New(fake.New())
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/instances/demo/start", nil)
	req.Header.Set("Origin", "http://evil.example")
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	assertStatus(t, res, http.StatusForbidden)
}

func TestCSRFGuardBlocksCrossSiteFetch(t *testing.T) {
	srv := New(fake.New())
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/instances/demo/start", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	assertStatus(t, res, http.StatusForbidden)
}

func TestCSRFGuardAllowsSameOriginPost(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))
	srv := New(b)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/instances/demo/start", nil)
	// httptest requests use host "example.com"; a same-origin browser POST
	// carries that as Origin plus Sec-Fetch-Site: same-origin.
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	assertStatus(t, res, http.StatusSeeOther)
}

func TestCSRFGuardIgnoresGets(t *testing.T) {
	srv := New(fake.New())
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	assertStatus(t, res, http.StatusOK)
}
