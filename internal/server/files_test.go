package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// demoFake returns a fake with one "demo" instance (which seeds /etc files).
func demoFake(t *testing.T) *fake.Fake {
	t.Helper()
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))
	return b
}

// uploadRequest posts a multipart file upload to the files endpoint.
func uploadRequest(t *testing.T, srv *http.Server, urlPath, dir, filename, content string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	require.NoError(t, mw.WriteField("path", dir))
	if filename != "" {
		fw, err := mw.CreateFormFile("file", filename)
		require.NoError(t, err)
		_, err = fw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, urlPath, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if htmx {
		req.Header.Set("Hx-Request", "true")
	}
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	return res
}

func TestFilesPanelListsRoot(t *testing.T) {
	res := request(t, New(demoFake(t)), "GET", "/instances/demo/files", "", true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "etc")
}

func TestFilesPanelListsSubdir(t *testing.T) {
	res := request(t, New(demoFake(t)), "GET", "/instances/demo/files?path=%2Fetc", "", true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "hostname")
}

func TestFilesPanelNormalizesDotSegments(t *testing.T) {
	// /etc/../etc cleans to /etc; doubled and trailing slashes are removed too.
	res := request(t, New(demoFake(t)), "GET", "/instances/demo/files?path=%2Fetc%2F..%2F%2Fetc%2F", "", true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "hostname")
}

func TestFilesPanelRelativePathIs400(t *testing.T) {
	res := request(t, New(demoFake(t)), "GET", "/instances/demo/files?path=etc", "", true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestFilesPanelGhostInstanceIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/instances/ghost/files", "", true)
	assertStatus(t, res, http.StatusNotFound)
}

func TestDownloadFileStreamsAttachment(t *testing.T) {
	res := request(t, New(demoFake(t)), "GET", "/instances/demo/files/download?path=%2Fetc%2Fhostname", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "demo")
	assert.Contains(t, res.Header().Get("Content-Disposition"), `filename="hostname"`)
}

func TestDownloadDirectoryIs400(t *testing.T) {
	res := request(t, New(demoFake(t)), "GET", "/instances/demo/files/download?path=%2Fetc", "", false)
	assertStatus(t, res, http.StatusBadRequest)
	assert.Empty(t, res.Header().Get("Content-Disposition"))
}

func TestDownloadGhostPathIs404(t *testing.T) {
	res := request(t, New(demoFake(t)), "GET", "/instances/demo/files/download?path=%2Fno%2Fsuch", "", false)
	assertStatus(t, res, http.StatusNotFound)
}

func TestUploadFileCreatesAndReturnsPanel(t *testing.T) {
	b := demoFake(t)
	res := uploadRequest(t, New(b), "/instances/demo/files/upload", "/root", "notes.txt", "hello", true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "notes.txt")

	var buf bytes.Buffer
	require.NoError(t, b.PullFile(t.Context(), "demo", "/root/notes.txt", &buf))
	assert.Equal(t, "hello", buf.String())
}

func TestUploadMissingFileIs400(t *testing.T) {
	res := uploadRequest(t, New(demoFake(t)), "/instances/demo/files/upload", "/root", "", "", true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestUploadRelativeDirIs400(t *testing.T) {
	res := uploadRequest(t, New(demoFake(t)), "/instances/demo/files/upload", "root", "notes.txt", "x", true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestUploadStripsClientPathFromFilename(t *testing.T) {
	b := demoFake(t)
	res := uploadRequest(t, New(b), "/instances/demo/files/upload", "/root", "dir/sub/notes.txt", "hi", true)
	assertStatus(t, res, http.StatusOK)

	var buf bytes.Buffer
	require.NoError(t, b.PullFile(t.Context(), "demo", "/root/notes.txt", &buf))
	assert.Equal(t, "hi", buf.String())
}

func TestUploadTooLargeIs413(t *testing.T) {
	old := maxFileUploadBytes
	maxFileUploadBytes = 16
	t.Cleanup(func() { maxFileUploadBytes = old })

	res := uploadRequest(t, New(demoFake(t)), "/instances/demo/files/upload", "/root", "big.bin", strings.Repeat("x", 4096), true)
	assertStatus(t, res, http.StatusRequestEntityTooLarge)
}
