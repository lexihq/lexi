package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoragePoolsPageLists(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/storage", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "default")
}

func TestStoragePoolPageRenders(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/storage/default", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, "dir")
	assert.Contains(t, body, "Volumes")
}

func TestStoragePoolUnknownIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/storage/ghost", "", false)
	assertStatus(t, res, http.StatusNotFound)
}

func TestPoolCreateFormRenders(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/storage/new", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, "Create pool")
	assert.Contains(t, body, `name="driver"`)
}

func TestCreatePoolAppliesAndRedirects(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/storage",
		url.Values{"name": {"scratch"}, "driver": {"dir"}, "description": {"made in test"},
			"key": {"source", ""}, "value": {"/tmp/scratch", ""}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/storage", res.Header().Get("Location"))

	p, err := b.GetStoragePool(t.Context(), "scratch")
	require.NoError(t, err)
	assert.Equal(t, "dir", p.Driver)
	assert.Equal(t, "made in test", p.Description)
	assert.Equal(t, "/tmp/scratch", p.Config["source"])
}

func TestCreatePoolBlankNameIs400(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/storage", url.Values{"name": {" "}, "driver": {"dir"}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestDeletePoolRedirects(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/storage/zfs0/delete", url.Values{}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/storage", res.Header().Get("Location"))
	_, err := b.GetStoragePool(t.Context(), "zfs0")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestDeletePoolInUseIs409(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/storage/default/delete", url.Values{}, false)
	assertStatus(t, res, http.StatusConflict)
}

func TestCreateVolumeAppliesAndRedirects(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/storage/default/volumes",
		url.Values{"name": {"vol1"}, "content_type": {"filesystem"},
			"key": {"size", ""}, "value": {"1GiB", ""}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	v, err := b.GetVolume(t.Context(), "default", "vol1")
	require.NoError(t, err)
	assert.Equal(t, "1GiB", v.Config["size"])
}

func TestCreateVolumeHTMXReturnsTablePartial(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/storage/default/volumes",
		url.Values{"name": {"vol1"}, "content_type": {"filesystem"}}, true)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, `id="volumes"`)
	assert.Contains(t, body, "vol1")
	// Must be the swappable partial, not the full page shell (which would nest a
	// second app layout inside #volumes after the htmx swap).
	assert.NotContains(t, body, "<!doctype")
}

func TestCreateVolumeBlankNameIs400(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/storage/default/volumes",
		url.Values{"name": {"  "}, "content_type": {"filesystem"}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestDeleteVolumeReturnsTable(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateVolume(t.Context(), "default", backend.StorageVolume{Name: "vol1", ContentType: "filesystem"}))
	res := formRequest(t, New(b), "/storage/default/volumes/vol1/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)
	_, err := b.GetVolume(t.Context(), "default", "vol1")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func newVolume(t *testing.T) *fake.Fake {
	t.Helper()
	b := fake.New()
	require.NoError(t, b.CreateVolume(t.Context(), "default", backend.StorageVolume{Name: "vol1", ContentType: "filesystem"}))
	return b
}

func TestStorageVolumePageRenders(t *testing.T) {
	res := request(t, New(newVolume(t)), "GET", "/storage/default/volumes/vol1", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, "vol1")
	assert.Contains(t, body, "Snapshots")
}

func TestStorageVolumeUnknownIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/storage/default/volumes/ghost", "", false)
	assertStatus(t, res, http.StatusNotFound)
}

func TestCreateVolumeSnapshotReturnsTable(t *testing.T) {
	b := newVolume(t)
	res := formRequest(t, New(b), "/storage/default/volumes/vol1/snapshots",
		url.Values{"snapshot": {"snap0"}}, true)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, `id="volume-snapshots"`)
	assert.Contains(t, body, "snap0")
	assert.NotContains(t, body, "<!doctype")
}

func TestCreateVolumeSnapshotBlankNameIs400(t *testing.T) {
	res := formRequest(t, New(newVolume(t)), "/storage/default/volumes/vol1/snapshots",
		url.Values{"snapshot": {"  "}}, true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestRestoreVolumeSnapshotReturnsTable(t *testing.T) {
	b := newVolume(t)
	require.NoError(t, b.CreateVolumeSnapshot(t.Context(), "default", "vol1", "snap0"))
	res := formRequest(t, New(b), "/storage/default/volumes/vol1/snapshots/snap0/restore", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "snap0")
}

func TestDeleteVolumeSnapshotReturnsTable(t *testing.T) {
	b := newVolume(t)
	require.NoError(t, b.CreateVolumeSnapshot(t.Context(), "default", "vol1", "snap0"))
	res := formRequest(t, New(b), "/storage/default/volumes/vol1/snapshots/snap0/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)
	snaps, err := b.ListVolumeSnapshots(t.Context(), "default", "vol1")
	require.NoError(t, err)
	assert.Empty(t, snaps)
}

func TestUpdatePoolAppliesAndRedirects(t *testing.T) {
	b := fake.New()
	p, err := b.GetStoragePool(t.Context(), "default")
	require.NoError(t, err)

	res := formRequest(t, New(b), "/storage/default/config",
		url.Values{"description": {"edited"}, "version": {string(p.Version)},
			"key": {"rsync.bwlimit", ""}, "value": {"10MiB", ""}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/storage/default", res.Header().Get("Location"))

	got, err := b.GetStoragePool(t.Context(), "default")
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, map[string]string{"rsync.bwlimit": "10MiB"}, got.Config)
}

func TestUpdatePoolStaleVersionIs409(t *testing.T) {
	b := fake.New()
	p, err := b.GetStoragePool(t.Context(), "default")
	require.NoError(t, err)
	require.NoError(t, b.UpdateStoragePool(t.Context(), "default", "racer", nil, p.Version))

	res := formRequest(t, New(b), "/storage/default/config",
		url.Values{"description": {"stale"}, "version": {string(p.Version)}}, true)
	assertStatus(t, res, http.StatusConflict)
}

func TestUpdatePoolGhostIs404(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/storage/ghost/config", url.Values{}, true)
	assertStatus(t, res, http.StatusNotFound)
}

func TestUpdateVolumeAppliesAndRedirects(t *testing.T) {
	b := newVolume(t)
	v, err := b.GetVolume(t.Context(), "default", "vol1")
	require.NoError(t, err)

	res := formRequest(t, New(b), "/storage/default/volumes/vol1/config",
		url.Values{"description": {"edited"}, "version": {string(v.Version)},
			"key": {"size", ""}, "value": {"2GiB", ""}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/storage/default/volumes/vol1", res.Header().Get("Location"))

	got, err := b.GetVolume(t.Context(), "default", "vol1")
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, "2GiB", got.Config["size"])
}

func TestUpdateVolumeStaleVersionIs409(t *testing.T) {
	b := newVolume(t)
	v, err := b.GetVolume(t.Context(), "default", "vol1")
	require.NoError(t, err)
	// The racer must change config: the volume etag ignores the description,
	// so only a config change invalidates the outstanding token.
	require.NoError(t, b.UpdateVolume(t.Context(), "default", "vol1", "racer", map[string]string{"size": "9GiB"}, v.Version))

	res := formRequest(t, New(b), "/storage/default/volumes/vol1/config",
		url.Values{"description": {"stale"}, "version": {string(v.Version)}}, true)
	assertStatus(t, res, http.StatusConflict)
}

func TestRenameVolumeRedirectsToNewName(t *testing.T) {
	b := newVolume(t)
	res := formRequest(t, New(b), "/storage/default/volumes/vol1/rename",
		url.Values{"new_name": {"vol2"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/storage/default/volumes/vol2", res.Header().Get("Location"))
	_, err := b.GetVolume(t.Context(), "default", "vol2")
	require.NoError(t, err)
}

func TestRenameVolumeBlankNameIs400(t *testing.T) {
	res := formRequest(t, New(newVolume(t)), "/storage/default/volumes/vol1/rename",
		url.Values{"new_name": {" "}}, true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestRenameVolumeSnapshotReturnsTable(t *testing.T) {
	b := newVolume(t)
	require.NoError(t, b.CreateVolumeSnapshot(t.Context(), "default", "vol1", "snap0"))
	res := formRequest(t, New(b), "/storage/default/volumes/vol1/snapshots/snap0/rename",
		url.Values{"new_name": {"renamed"}}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "renamed")

	snaps, err := b.ListVolumeSnapshots(t.Context(), "default", "vol1")
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "renamed", snaps[0].Name)
}

func TestRenameVolumeSnapshotBlankNameIs400(t *testing.T) {
	b := newVolume(t)
	require.NoError(t, b.CreateVolumeSnapshot(t.Context(), "default", "vol1", "snap0"))
	res := formRequest(t, New(b), "/storage/default/volumes/vol1/snapshots/snap0/rename",
		url.Values{"new_name": {"  "}}, true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestUpdateVolumeSnapshotExpiryReturnsTable(t *testing.T) {
	b := newVolume(t)
	require.NoError(t, b.CreateVolumeSnapshot(t.Context(), "default", "vol1", "snap0"))
	res := formRequest(t, New(b), "/storage/default/volumes/vol1/snapshots/snap0/expiry",
		url.Values{"expires_at": {"2031-01-02T03:04"}}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "2031-01-02 03:04 UTC")

	snaps, err := b.ListVolumeSnapshots(t.Context(), "default", "vol1")
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "2031-01-02 03:04", snaps[0].ExpiresAt.UTC().Format("2006-01-02 15:04"))
}

func TestExportVolumeDownloads(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateVolume(t.Context(), "default", backend.StorageVolume{
		Name: "vol1", Description: "scratch", Config: map[string]string{"size": "1GiB"},
	}))

	res := request(t, New(b), "GET", "/storage/default/volumes/vol1/export", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Header().Get("Content-Disposition"), "default-vol1.tar.gz")
	assert.Contains(t, res.Body.String(), "scratch", "fake export blob carries the description")
}

func TestExportVolumeGhostIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/storage/default/volumes/ghost/export", "", false)
	assertStatus(t, res, http.StatusNotFound)
	assert.Empty(t, res.Header().Get("Content-Disposition"), "no attachment headers on error")
}

// importVolumeRequest posts a multipart volume backup upload.
func importVolumeRequest(t *testing.T, srv *http.Server, pool, name, content string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if name != "" {
		require.NoError(t, mw.WriteField("name", name))
	}
	fw, err := mw.CreateFormFile("backup", "volume.tar.gz")
	require.NoError(t, err)
	_, err = fw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/storage/"+pool+"/volumes/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	return res
}

func TestImportVolumeRoundTrip(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateVolume(t.Context(), "default", backend.StorageVolume{
		Name: "vol1", Description: "scratch", Config: map[string]string{"size": "1GiB"},
	}))
	srv := New(b)

	export := request(t, srv, "GET", "/storage/default/volumes/vol1/export", "", false)
	assertStatus(t, export, http.StatusOK)

	res := importVolumeRequest(t, srv, "default", "restored", export.Body.String())
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/storage/default", res.Header().Get("Location"))

	got, err := b.GetVolume(t.Context(), "default", "restored")
	require.NoError(t, err)
	assert.Equal(t, "scratch", got.Description)
	assert.Equal(t, "1GiB", got.Config["size"])
}

func TestImportVolumeForeignBlobIs400(t *testing.T) {
	res := importVolumeRequest(t, New(fake.New()), "default", "v", "garbage")
	assertStatus(t, res, http.StatusBadRequest)
}

func TestImportVolumeMissingNameIs400(t *testing.T) {
	res := importVolumeRequest(t, New(fake.New()), "default", "", "whatever")
	assertStatus(t, res, http.StatusBadRequest)
}

// uploadISORequest posts a multipart ISO volume upload.
func uploadISORequest(t *testing.T, srv *http.Server, pool, name, content string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if name != "" {
		require.NoError(t, mw.WriteField("name", name))
	}
	fw, err := mw.CreateFormFile("iso", "install.iso")
	require.NoError(t, err)
	_, err = fw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/storage/"+pool+"/volumes/iso", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	return res
}

func TestUploadISOVolume(t *testing.T) {
	b := fake.New()
	res := uploadISORequest(t, New(b), "default", "install-media", "iso-bytes")
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/storage/default", res.Header().Get("Location"))

	got, err := b.GetVolume(t.Context(), "default", "install-media")
	require.NoError(t, err)
	assert.Equal(t, "iso", got.ContentType)
}

func TestUploadISOVolumeMissingNameIs400(t *testing.T) {
	res := uploadISORequest(t, New(fake.New()), "default", "", "iso-bytes")
	assertStatus(t, res, http.StatusBadRequest)
}

func TestUploadISOVolumeDuplicateIs409(t *testing.T) {
	b := fake.New()
	srv := New(b)
	assertStatus(t, uploadISORequest(t, srv, "default", "install-media", "iso-bytes"), http.StatusSeeOther)
	assertStatus(t, uploadISORequest(t, srv, "default", "install-media", "iso-bytes"), http.StatusConflict)
}

func TestUploadISOVolumeGhostPoolIs404(t *testing.T) {
	res := uploadISORequest(t, New(fake.New()), "ghost", "install-media", "iso-bytes")
	assertStatus(t, res, http.StatusNotFound)
}
