package server

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"

	"github.com/gorilla/websocket"
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
	form := url.Values{"name": {"demo"}, "image": {"fake-debian-12-aarch64"}, "start": {"on"}}

	res := formRequest(t, New(b), "/instances", form, true)

	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	if !strings.Contains(body, "demo") || !strings.Contains(body, "Running") {
		t.Fatalf("expected created running row, got %q", body)
	}
	inst, err := b.GetInstance(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "debian/12", inst.Image)
}

func TestCreateRejectsUnknownImageFingerprint(t *testing.T) {
	form := url.Values{"name": {"demo"}, "image": {"unknown-fingerprint"}}

	res := formRequest(t, New(fake.New()), "/instances", form, true)

	assert.Equal(t, http.StatusBadRequest, res.Code)
	assert.Contains(t, res.Body.String(), "selected image is unavailable")
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
	dup := formRequest(t, New(b), "/instances", url.Values{"name": {"demo"}, "image": {"fake-debian-12-aarch64"}}, true)
	assert.Equal(t, http.StatusConflict, dup.Code)

	assert.Equal(t, http.StatusBadRequest, statusFor(fmt.Errorf("invalid limits: %w", backend.ErrInvalid)))
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
		assert.Contains(t, body, "ubuntu/24.04")
		assert.NotContains(t, body, "alpine/edge")
	})

	t.Run("by type", func(t *testing.T) {
		res := request(t, srv, "GET", "/images?type=virtual-machine", "", true)
		assert.Equal(t, http.StatusOK, res.Code)
		body := res.Body.String()
		assert.Contains(t, body, "ubuntu/24.04")
		assert.Contains(t, body, "virtual-machine")
		assert.NotContains(t, body, "debian/12")
	})
}

func TestUpdateLimitsHXReturnsForm(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := formRequest(t, New(b), "/instances/demo/limits", url.Values{"cpu": {"2"}, "memory": {"2GiB"}}, true)

	assert.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	assert.Contains(t, body, "2GiB")
	assert.Contains(t, body, `value="2"`)

	inst, err := b.GetInstance(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "2", inst.LimitsCPU)
	assert.Equal(t, "2GiB", inst.LimitsMemory)
}

func TestMetricsReturnsPanel(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), "GET", "/instances/demo/metrics", "", true)

	assert.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	assert.Contains(t, body, "Live metrics")
	assert.Contains(t, body, "256.0 MiB")
	assert.Contains(t, body, "12.5%")
}

func TestMetricsUnknownInstanceIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/instances/ghost/metrics", "", true)
	assert.Equal(t, http.StatusNotFound, res.Code)
}

func TestLogsReturnsPanel(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), "GET", "/instances/demo/logs", "", true)

	assert.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	assert.Contains(t, body, "Console log")
	assert.Contains(t, body, "demo booted")
}

func TestLogsUnknownInstanceIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/instances/ghost/logs", "", true)
	assert.Equal(t, http.StatusNotFound, res.Code)
}

func TestConsolePageRenders(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), "GET", "/instances/demo/console", "", false)

	assert.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	assert.Contains(t, body, "/static/js/xterm.js")
	assert.Contains(t, body, "/static/js/console.js")
	assert.Contains(t, body, "/instances/demo/console/ws")
}

func TestConsoleWSBridgesExec(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	httpSrv := httptest.NewServer(New(b).Handler)
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/instances/demo/console/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Binary stdin is echoed back by the fake as binary stdout.
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("hello\n")))
	mt, data, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.BinaryMessage, mt)
	assert.Equal(t, "hello\n", string(data))

	// A resize control frame (text JSON) is accepted; the session stays live.
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"cols":100,"rows":30}`)))
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("world\n")))
	_, data, err = conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "world\n", string(data))
}

func TestExportDownloadsTarball(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), "GET", "/instances/demo/export", "", false)

	assert.Equal(t, http.StatusOK, res.Code)
	assert.Equal(t, "application/octet-stream", res.Header().Get("Content-Type"))
	assert.Contains(t, res.Header().Get("Content-Disposition"), `filename="demo.tar.gz"`)
	assert.NotEmpty(t, res.Body.Bytes())
}

func TestExportUnknownInstanceIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/instances/ghost/export", "", false)
	assert.Equal(t, http.StatusNotFound, res.Code)
}

func TestImportFormRenders(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/instances/import", "", false)
	assert.Equal(t, http.StatusOK, res.Code)
	assert.Contains(t, res.Body.String(), "Import instance")
}

func TestImportCreatesInstanceFromUpload(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))
	var buf bytes.Buffer
	require.NoError(t, b.ExportInstance(t.Context(), "demo", &buf))

	res := importRequest(t, New(b), "restored", buf.Bytes(), false)

	assert.Equal(t, http.StatusSeeOther, res.Code)
	assert.Equal(t, "/", res.Header().Get("Location"))
	inst, err := b.GetInstance(t.Context(), "restored")
	require.NoError(t, err)
	assert.Equal(t, "debian/12", inst.Image)
}

func TestImportValidatesNameAndFile(t *testing.T) {
	t.Run("missing name", func(t *testing.T) {
		res := importRequest(t, New(fake.New()), "", []byte("x"), true)
		assert.Equal(t, http.StatusBadRequest, res.Code)
		assert.Contains(t, res.Body.String(), "name is required")
	})

	t.Run("missing file", func(t *testing.T) {
		res := importRequest(t, New(fake.New()), "restored", nil, true)
		assert.Equal(t, http.StatusBadRequest, res.Code)
		assert.Contains(t, res.Body.String(), "backup file is required")
	})
}

func TestImportRejectsInvalidBackup(t *testing.T) {
	res := importRequest(t, New(fake.New()), "restored", []byte("garbage"), true)
	assert.Equal(t, http.StatusBadRequest, res.Code)
}

func TestImportRejectsOversizedUpload(t *testing.T) {
	orig := maxImportBytes
	maxImportBytes = 16
	t.Cleanup(func() { maxImportBytes = orig })

	res := importRequest(t, New(fake.New()), "restored", make([]byte, 1<<10), true)

	assert.Equal(t, http.StatusRequestEntityTooLarge, res.Code)
	assert.Contains(t, res.Body.String(), "too large")
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

	req := httptest.NewRequest("POST", "/instances/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
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
