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
