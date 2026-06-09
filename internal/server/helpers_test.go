package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Shared test fixtures for the server package: thin HTTP-request builders over a
// configured *http.Server and a status assertion. Used across every per-feature
// *_test.go in this package.

func request(t *testing.T, srv *http.Server, method, path, body string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if htmx {
		req.Header.Set("Hx-Request", "true")
	}
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	return res
}

// boostedRequest issues a GET carrying both HX-Request and HX-Boosted, as an
// hx-boost navigation does.
func boostedRequest(t *testing.T, srv *http.Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	req.Header.Set("Hx-Request", "true")
	req.Header.Set("Hx-Boosted", "true")
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	return res
}

func formRequest(t *testing.T, srv *http.Server, path string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("Hx-Request", "true")
	}
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	return res
}

// importRequest posts a multipart upload to the import endpoint. An empty name
// omits the field and a nil file omits the file part, so the helper can drive
// the validation paths as well as a successful upload.
func importRequest(t *testing.T, srv *http.Server, name string, file []byte, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if name != "" {
		require.NoError(t, mw.WriteField("name", name))
	}
	if file != nil {
		fw, err := mw.CreateFormFile("backup", "backup.tar.gz")
		require.NoError(t, err)
		_, err = fw.Write(file)
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/instances/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if htmx {
		req.Header.Set("Hx-Request", "true")
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
