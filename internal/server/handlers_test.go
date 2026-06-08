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

func TestStartHXReturnsUpdatedRow(t *testing.T) {
	b := fake.New()
	if err := b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}); err != nil {
		t.Fatal(err)
	}

	res := request(t, New(b), "POST", "/instances/demo/start", "", true)

	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	if !strings.Contains(body, "demo") || !strings.Contains(body, "Running") {
		t.Fatalf("expected updated row with running demo, got %q", body)
	}
	inst, err := b.GetInstance(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status != "Running" {
		t.Fatalf("expected backend status Running, got %q", inst.Status)
	}
}

func TestDeleteHXRemovesRow(t *testing.T) {
	b := fake.New()
	if err := b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}); err != nil {
		t.Fatal(err)
	}

	res := request(t, New(b), "POST", "/instances/demo/delete", "", true)

	assertStatus(t, res, http.StatusOK)
	if body := strings.TrimSpace(res.Body.String()); body != "" {
		t.Fatalf("expected empty htmx delete body, got %q", body)
	}
	instances, err := b.ListInstances(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Fatalf("expected deleted instance, got %#v", instances)
	}
}

func TestCreateValidatesNameAndImage(t *testing.T) {
	tests := []struct {
		name string
		form url.Values
		want string
	}{
		{name: "missing name", form: url.Values{"image": {"debian/12"}}, want: "name is required"},
		{name: "missing image", form: url.Values{"name": {"demo"}}, want: "image is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := formRequest(t, New(fake.New()), "/instances", tt.form, true)

			assertStatus(t, res, http.StatusBadRequest)
			if body := res.Body.String(); !strings.Contains(body, tt.want) {
				t.Fatalf("expected %q in response, got %q", tt.want, body)
			}
		})
	}
}

func TestCreateHXReturnsCreatedRow(t *testing.T) {
	b := fake.New()
	form := url.Values{"name": {"demo"}, "image": {"debian/12"}, "start": {"on"}}

	res := formRequest(t, New(b), "/instances", form, true)

	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	if !strings.Contains(body, "demo") || !strings.Contains(body, "Running") {
		t.Fatalf("expected created running row, got %q", body)
	}
}

func TestHXRequestTogglesPartialVsRedirect(t *testing.T) {
	b := fake.New()
	if err := b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}); err != nil {
		t.Fatal(err)
	}

	hx := request(t, New(b), "POST", "/instances/demo/start", "", true)
	assertStatus(t, hx, http.StatusOK)
	if body := hx.Body.String(); strings.Contains(strings.ToLower(body), "<!doctype") || !strings.Contains(body, "Running") {
		t.Fatalf("expected htmx partial row, got %q", body)
	}

	full := request(t, New(b), "POST", "/instances/demo/stop", "", false)
	assertStatus(t, full, http.StatusSeeOther)
	if loc := full.Header().Get("Location"); loc != "/" {
		t.Fatalf("expected redirect to /, got %q", loc)
	}
}

func TestStatusForSentinels(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	// Missing instance → 404.
	missing := request(t, New(b), "GET", "/instances/ghost", "", true)
	assert.Equal(t, http.StatusNotFound, missing.Code)

	// Duplicate create → 409.
	dup := formRequest(t, New(b), "/instances", url.Values{"name": {"demo"}, "image": {"debian/12"}}, true)
	assert.Equal(t, http.StatusConflict, dup.Code)
}

func TestImagesFilter(t *testing.T) {
	srv := New(fake.New())

	t.Run("by query matches distribution", func(t *testing.T) {
		res := request(t, srv, "GET", "/images?q=debian", "", true)
		assert.Equal(t, http.StatusOK, res.Code)
		body := res.Body.String()
		assert.Contains(t, body, "debian/12")
		assert.NotContains(t, body, "fedora/40")
		assert.NotContains(t, body, "alpine/edge")
	})

	t.Run("by arch", func(t *testing.T) {
		res := request(t, srv, "GET", "/images?arch=x86_64", "", true)
		assert.Equal(t, http.StatusOK, res.Code)
		body := res.Body.String()
		assert.Contains(t, body, "fedora/40")
		assert.Contains(t, body, "debian/12")
		assert.NotContains(t, body, "ubuntu/24.04")
		assert.NotContains(t, body, "alpine/edge")
	})
}

func request(t *testing.T, srv *http.Server, method, path, body string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	return res
}

func formRequest(t *testing.T, srv *http.Server, path string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	return res
}

func assertStatus(t *testing.T, res *httptest.ResponseRecorder, want int) {
	t.Helper()
	if res.Code != want {
		t.Fatalf("expected status %d, got %d with body %q", want, res.Code, res.Body.String())
	}
}
