package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
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
