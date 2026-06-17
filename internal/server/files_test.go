package server

import (
	"bytes"
	"mime/multipart"
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

func TestDeleteFileRemovesAndReturnsPanel(t *testing.T) {
	b := demoFake(t)
	res := request(t, New(b), "POST", "/instances/demo/files/delete?path=%2Fetc%2Fhostname", "", true)
	assertStatus(t, res, http.StatusOK)
	// The panel re-renders at the parent directory.
	assert.Contains(t, res.Body.String(), "os-release")
	assert.NotContains(t, res.Body.String(), "hostname")

	err := b.PullFile(t.Context(), "demo", "/etc/hostname", &bytes.Buffer{})
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestDeleteFileRelativePathIs400(t *testing.T) {
	res := request(t, New(demoFake(t)), "POST", "/instances/demo/files/delete?path=etc", "", true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestDeleteNonEmptyDirIs400(t *testing.T) {
	res := request(t, New(demoFake(t)), "POST", "/instances/demo/files/delete?path=%2Fetc", "", true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestMkdirCreatesAndReturnsPanel(t *testing.T) {
	b := demoFake(t)
	res := request(t, New(b), "POST", "/instances/demo/files/mkdir?dir=%2F&name=data", "", true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "data")

	entries, err := b.ListFiles(t.Context(), "demo", "/data")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestMkdirBadNameIs400(t *testing.T) {
	for _, name := range []string{"", ".", "..", "a%2Fb"} {
		res := request(t, New(demoFake(t)), "POST", "/instances/demo/files/mkdir?dir=%2F&name="+name, "", true)
		assertStatus(t, res, http.StatusBadRequest)
	}
}

func TestMkdirExistingIs409(t *testing.T) {
	res := request(t, New(demoFake(t)), "POST", "/instances/demo/files/mkdir?dir=%2F&name=etc", "", true)
	assertStatus(t, res, http.StatusConflict)
}

func TestEditFileFormShowsContentAndMetadata(t *testing.T) {
	b := demoFake(t)
	require.NoError(t, b.PushFile(t.Context(), "demo", "/root/app.conf", strings.NewReader("key=value\n"),
		backend.FileWriteOptions{Mode: "0600", UID: 1000, GID: 1000}))

	res := request(t, New(b), "GET", "/instances/demo/files/edit?path=%2Froot%2Fapp.conf", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, "key=value")
	assert.Contains(t, body, `name="mode" value="0600"`)
	assert.Contains(t, body, `name="uid" value="1000"`)
	assert.Contains(t, body, `name="gid" value="1000"`)
}

func TestEditFileFormDirectoryIs400(t *testing.T) {
	res := request(t, New(demoFake(t)), "GET", "/instances/demo/files/edit?path=%2Fetc", "", false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestEditFileFormSymlinkIs400(t *testing.T) {
	b := demoFake(t)
	b.SeedSymlink("demo", "/etc/localtime")
	res := request(t, New(b), "GET", "/instances/demo/files/edit?path=%2Fetc%2Flocaltime", "", false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestEditFileFormBinaryIs400(t *testing.T) {
	b := demoFake(t)
	require.NoError(t, b.PushFile(t.Context(), "demo", "/root/blob.bin", strings.NewReader("ab\x00cd"),
		backend.FileWriteOptions{}))
	res := request(t, New(b), "GET", "/instances/demo/files/edit?path=%2Froot%2Fblob.bin", "", false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestEditFileFormTooLargeIs400(t *testing.T) {
	b := demoFake(t)
	big := strings.Repeat("x", (1<<20)+1)
	require.NoError(t, b.PushFile(t.Context(), "demo", "/root/big.txt", strings.NewReader(big),
		backend.FileWriteOptions{}))
	res := request(t, New(b), "GET", "/instances/demo/files/edit?path=%2Froot%2Fbig.txt", "", false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestViewFileShowsContent(t *testing.T) {
	res := request(t, New(demoFake(t)), "GET", "/instances/demo/files/view?path=%2Fetc%2Fos-release", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "Fake Linux")
}

func TestViewFileBinaryIsShownNotRefused(t *testing.T) {
	b := demoFake(t)
	require.NoError(t, b.PushFile(t.Context(), "demo", "/root/app.log", strings.NewReader("start\x00\xffend"),
		backend.FileWriteOptions{}))
	res := request(t, New(b), "GET", "/instances/demo/files/view?path=%2Froot%2Fapp.log", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.NotContains(t, res.Body.String(), "\x00", "NUL bytes should be sanitized out of the page")
}

func TestViewFileTruncatedShowsNotice(t *testing.T) {
	old := maxViewableFileBytes
	maxViewableFileBytes = 8
	t.Cleanup(func() { maxViewableFileBytes = old })

	b := demoFake(t)
	require.NoError(t, b.PushFile(t.Context(), "demo", "/root/big.log", strings.NewReader(strings.Repeat("x", 64)),
		backend.FileWriteOptions{}))
	res := request(t, New(b), "GET", "/instances/demo/files/view?path=%2Froot%2Fbig.log", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "download it for the full")
}

func TestViewFileEscapesHTML(t *testing.T) {
	b := demoFake(t)
	require.NoError(t, b.PushFile(t.Context(), "demo", "/root/x.log",
		strings.NewReader("</pre><script>alert(1)</script>"), backend.FileWriteOptions{}))
	res := request(t, New(b), "GET", "/instances/demo/files/view?path=%2Froot%2Fx.log", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.NotContains(t, body, "<script>alert(1)</script>", "file content must not be rendered as live markup")
	assert.Contains(t, body, "&lt;script&gt;", "content should be HTML-escaped")
}

func TestViewFileDirectoryIs400(t *testing.T) {
	res := request(t, New(demoFake(t)), "GET", "/instances/demo/files/view?path=%2Fetc", "", false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestSaveFilePreservesMetadataAndRedirects(t *testing.T) {
	b := demoFake(t)
	require.NoError(t, b.PushFile(t.Context(), "demo", "/root/app.conf", strings.NewReader("old\n"),
		backend.FileWriteOptions{Mode: "0600", UID: 1000, GID: 1000}))

	form := url.Values{"content": {"new\r\ncontents\r\n"}, "mode": {"0600"}, "uid": {"1000"}, "gid": {"1000"}}
	res := formRequest(t, New(b), "/instances/demo/files/edit?path=%2Froot%2Fapp.conf", form, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Contains(t, res.Header().Get("Location"), "tab=files")

	var buf bytes.Buffer
	info, err := b.PullFileInfo(t.Context(), "demo", "/root/app.conf", &buf, 0)
	require.NoError(t, err)
	assert.Equal(t, "new\ncontents\n", buf.String())
	assert.Equal(t, backend.FileInfo{Type: "file", Mode: "0600", UID: 1000, GID: 1000}, info)
}

func TestSaveFileBadOwnershipIs400(t *testing.T) {
	for _, form := range []url.Values{
		{"content": {"x"}, "mode": {"0644"}, "uid": {"abc"}, "gid": {"0"}},
		{"content": {"x"}, "mode": {"0644"}, "uid": {"-1"}, "gid": {"0"}},
		{"content": {"x"}, "mode": {"0644"}, "uid": {"0"}, "gid": {"-5"}},
		{"content": {"x"}, "mode": {"rwxr"}, "uid": {"0"}, "gid": {"0"}},
		{"content": {"x"}, "mode": {"7777"}, "uid": {"0"}, "gid": {"0"}},
	} {
		res := formRequest(t, New(demoFake(t)), "/instances/demo/files/edit?path=%2Fetc%2Fhostname", form, false)
		assertStatus(t, res, http.StatusBadRequest)
	}
}

func TestSaveFileBinaryContentIs400(t *testing.T) {
	form := url.Values{"content": {"ab\x00cd"}, "mode": {"0644"}, "uid": {"0"}, "gid": {"0"}}
	res := formRequest(t, New(demoFake(t)), "/instances/demo/files/edit?path=%2Fetc%2Fhostname", form, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestSaveFileTooLargeIs413(t *testing.T) {
	form := url.Values{"content": {strings.Repeat("x", (1<<20)+1)}, "mode": {"0644"}, "uid": {"0"}, "gid": {"0"}}
	res := formRequest(t, New(demoFake(t)), "/instances/demo/files/edit?path=%2Fetc%2Fhostname", form, false)
	assertStatus(t, res, http.StatusRequestEntityTooLarge)
}

func TestUploadTooLargeIs413(t *testing.T) {
	old := maxFileUploadBytes
	maxFileUploadBytes = 16
	t.Cleanup(func() { maxFileUploadBytes = old })

	res := uploadRequest(t, New(demoFake(t)), "/instances/demo/files/upload", "/root", "big.bin", strings.Repeat("x", 4096), true)
	assertStatus(t, res, http.StatusRequestEntityTooLarge)
}
