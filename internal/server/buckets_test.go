package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoragePoolPageRendersBuckets(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateBucket(t.Context(), "default", "media", "app assets", "100MiB"))

	res := request(t, New(b), "GET", "/storage/default", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, "Buckets")
	assert.Contains(t, body, "media")
	assert.Contains(t, body, "https://fake-s3:8555/media")
	assert.Contains(t, body, "100MiB")
	assert.Contains(t, body, "FAKEACCESS")                                   // seeded admin key credentials
	assert.Contains(t, body, `action="/storage/default/buckets"`)            // create form
	assert.Contains(t, body, `action="/storage/default/buckets/media/keys"`) // add-key form
}

func TestCreateBucketAppliesAndRedirects(t *testing.T) {
	b := fake.New()
	form := url.Values{"name": {"media"}, "description": {"d"}, "size": {"100MiB"}}
	res := formRequest(t, New(b), "/storage/default/buckets", form, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/storage/default", res.Header().Get("Location"))

	buckets, err := b.ListBuckets(t.Context(), "default")
	require.NoError(t, err)
	require.Len(t, buckets, 1)
	assert.Equal(t, "100MiB", buckets[0].Size)

	res = formRequest(t, New(b), "/storage/default/buckets", url.Values{"name": {""}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestBucketKeyAddValidatesRole(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateBucket(t.Context(), "default", "media", "", ""))
	srv := New(b)

	form := url.Values{"name": {"ci"}, "role": {"read-only"}, "description": {"ci reader"}}
	res := formRequest(t, srv, "/storage/default/buckets/media/keys", form, false)
	assertStatus(t, res, http.StatusSeeOther)

	keys, err := b.ListBucketKeys(t.Context(), "default", "media")
	require.NoError(t, err)
	require.Len(t, keys, 2) // admin + ci

	res = formRequest(t, srv, "/storage/default/buckets/media/keys",
		url.Values{"name": {"x"}, "role": {"owner"}}, false)
	assertStatus(t, res, http.StatusBadRequest)
	res = formRequest(t, srv, "/storage/default/buckets/media/keys",
		url.Values{"name": {""}, "role": {"admin"}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestBucketAndKeyDelete(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateBucket(t.Context(), "default", "media", "", ""))
	srv := New(b)

	res := formRequest(t, srv, "/storage/default/buckets/media/keys/delete", url.Values{"key": {"admin"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	keys, err := b.ListBucketKeys(t.Context(), "default", "media")
	require.NoError(t, err)
	assert.Empty(t, keys)

	res = formRequest(t, srv, "/storage/default/buckets/media/delete", url.Values{}, false)
	assertStatus(t, res, http.StatusSeeOther)
	buckets, err := b.ListBuckets(t.Context(), "default")
	require.NoError(t, err)
	assert.Empty(t, buckets)

	res = formRequest(t, srv, "/storage/default/buckets/ghost/delete", url.Values{}, false)
	assertStatus(t, res, http.StatusNotFound)
}
