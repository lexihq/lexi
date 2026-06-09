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

func TestRestartPauseResumeReturnRowOnHTMX(t *testing.T) {
	cases := []struct {
		path   string
		status string // status reflected in the returned row
	}{
		{"/instances/demo/restart", "Running"},
		{"/instances/demo/pause", "Frozen"},
		{"/instances/demo/resume", "Running"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			b := fake.New()
			if err := b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Start: true}); err != nil {
				t.Fatal(err)
			}

			res := request(t, New(b), "POST", tc.path, "", true)

			assertStatus(t, res, http.StatusOK)
			body := res.Body.String()
			assert.Contains(t, body, `id="instance-demo"`)
			assert.Contains(t, body, tc.status)
		})
	}
}

func TestLifecycleActionUnknownInstanceIs404(t *testing.T) {
	b := fake.New()
	res := request(t, New(b), "POST", "/instances/ghost/restart", "", true)
	assertStatus(t, res, http.StatusNotFound)
}

func TestMergeProfileOrderKeepsExistingThenAppends(t *testing.T) {
	assert.Equal(t, []string{"default", "gpu"},
		mergeProfileOrder([]string{"default", "gpu"}, []string{"gpu", "default"}))
	assert.Equal(t, []string{"default", "gpu", "web"},
		mergeProfileOrder([]string{"default", "gpu"}, []string{"gpu", "web", "default"}))
	assert.Equal(t, []string{"default"},
		mergeProfileOrder([]string{"default", "gpu"}, []string{"default"}))
}

func TestProfilesPageRenders(t *testing.T) {
	b := fake.New()
	res := request(t, New(b), "GET", "/profiles", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, "default")
	assert.Contains(t, body, "gpu")
}

func TestProfileDetailRenders(t *testing.T) {
	b := fake.New()
	res := request(t, New(b), "GET", "/profiles/gpu", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "gpu")
}

func TestSetInstanceProfilesReturnsUpdatedControlOnHTMX(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	res := formRequest(t, New(b), "/instances/demo/profiles", url.Values{"profile": {"default", "gpu"}}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "gpu")
	inst, err := b.GetInstance(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"default", "gpu"}, inst.Profiles)
}

func TestSetInstanceProfilesUnknownInstanceIs404(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/instances/ghost/profiles", url.Values{"profile": {"default"}}, true)
	assertStatus(t, res, http.StatusNotFound)
}

func TestZipConfigPairsDropsBlankKeys(t *testing.T) {
	got := zipConfigPairs([]string{"security.nesting", "", "boot.autostart"}, []string{"true", "ignored", "1"})
	assert.Equal(t, map[string]string{"security.nesting": "true", "boot.autostart": "1"}, got)

	got = zipConfigPairs([]string{"a", "b"}, []string{"x"})
	assert.Equal(t, map[string]string{"a": "x", "b": ""}, got)
}

func TestConfigPanelRenders(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	require.NoError(t, b.UpdateInstanceConfig(t.Context(), "demo", map[string]string{"security.nesting": "true"}))
	res := request(t, New(b), "GET", "/instances/demo/config", "", true)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, "security.nesting")
	assert.Contains(t, body, "root") // device from default profile
}

func TestUpdateConfigAppliesAndReturnsPanel(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	res := formRequest(t, New(b), "/instances/demo/config",
		url.Values{"key": {"security.nesting", ""}, "value": {"true", ""}}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "security.nesting")
	cfg, err := b.GetInstanceConfig(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "true", cfg.Config["security.nesting"])
}

func TestConfigPanelUnknownInstanceIs404(t *testing.T) {
	b := fake.New()
	res := request(t, New(b), "GET", "/instances/ghost/config", "", true)
	assertStatus(t, res, http.StatusNotFound)
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

func TestInstanceURLPathEscapesName(t *testing.T) {
	assert.Equal(t, "/instances/demo", instanceURL("demo"))
	assert.Equal(t, "/instances/%2F%2Fevil.example", instanceURL("//evil.example"))
}

func TestSidebarPartialListsInstancesWithActive(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), "GET", "/partials/sidebar?active=demo", "", true)

	assert.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	assert.Contains(t, body, "demo")
	assert.Contains(t, body, "bg-accent")           // active=demo highlight threaded through
	assert.Contains(t, body, "bg-muted-foreground") // stopped status dot
}

func TestDetailTabReturnsFragmentForHXAndFullPageOtherwise(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))
	srv := New(b)

	// HTMX request gets just the swappable body (no shell), with the requested
	// tab mounted — proving ?tab is forwarded and the fragment branch is taken.
	hx := request(t, srv, "GET", "/instances/demo?tab=metrics", "", true)
	assert.Equal(t, http.StatusOK, hx.Code)
	hxBody := hx.Body.String()
	assert.NotContains(t, strings.ToLower(hxBody), "<!doctype")
	assert.Contains(t, hxBody, `id="instance-body"`)
	assert.Contains(t, hxBody, `hx-get="/instances/demo/metrics"`)

	// A plain request gets the full shell (doctype + sidebar) for reload/deep-link.
	full := request(t, srv, "GET", "/instances/demo?tab=metrics", "", false)
	assert.Equal(t, http.StatusOK, full.Code)
	fullBody := full.Body.String()
	assert.Contains(t, strings.ToLower(fullBody), "<!doctype")
	assert.Contains(t, fullBody, "/partials/sidebar")
	assert.Contains(t, fullBody, `id="instance-body"`)

	// A boosted navigation (HX-Request + HX-Boosted) must get the full page too,
	// so hx-boost swaps the whole shell — not the bare tab fragment.
	boosted := boostedRequest(t, srv, "/instances/demo?tab=metrics")
	assert.Equal(t, http.StatusOK, boosted.Code)
	boostedBody := boosted.Body.String()
	assert.Contains(t, strings.ToLower(boostedBody), "<!doctype")
	assert.Contains(t, boostedBody, "/partials/sidebar")
	assert.Contains(t, boostedBody, `id="instance-body"`)
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
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})

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
