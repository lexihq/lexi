package server

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// localAliases flattens the backend's local image aliases for assertions.
func localAliases(t *testing.T, b *fake.Fake) []string {
	t.Helper()
	imgs, err := b.ListLocalImages(t.Context())
	require.NoError(t, err)
	var out []string
	for _, img := range imgs {
		out = append(out, img.Aliases...)
	}
	return out
}

func TestImagesPageListsLocalImages(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/images", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "debian/12")
	assert.Contains(t, res.Body.String(), "fake-debian-12-aarch64")
}

func TestImagePickerPartialMovedToPartialsPath(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/partials/images?q=debian", "", true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "debian/12")
}

func TestCopyImageAppliesAndRedirects(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/images/copy", url.Values{"alias": {"alpine/edge"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Contains(t, localAliases(t, b), "alpine/edge")
}

func TestCopyImageHTMXReturnsTable(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/images/copy", url.Values{"alias": {"alpine/edge"}}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "alpine/edge")
}

func TestCopyImageBlankAliasIs400(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/images/copy", url.Values{"alias": {"  "}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestCopyImageGhostAliasIs404(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/images/copy", url.Values{"alias": {"no/such"}}, false)
	assertStatus(t, res, http.StatusNotFound)
}

func TestPublishImageAppliesAndRedirects(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))
	res := formRequest(t, New(b), "/images/publish",
		url.Values{"instance": {"demo"}, "alias": {"demo-snap"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Contains(t, localAliases(t, b), "demo-snap")
}

func TestPublishImageBlankInstanceIs400(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/images/publish", url.Values{"instance": {" "}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestPublishImageGhostInstanceIs404(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/images/publish", url.Values{"instance": {"ghost"}}, false)
	assertStatus(t, res, http.StatusNotFound)
}

func TestDeleteImageRemovesAndReturnsTable(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/images/fake-debian-12-aarch64/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)
	imgs, err := b.ListLocalImages(t.Context())
	require.NoError(t, err)
	assert.Empty(t, imgs)
}

func TestDeleteImageGhostIs404(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/images/no-such-fp/delete", url.Values{}, false)
	assertStatus(t, res, http.StatusNotFound)
}

func TestAddImageAliasApplies(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/images/fake-debian-12-aarch64/aliases",
		url.Values{"alias": {"extra"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Contains(t, localAliases(t, b), "extra")
}

func TestAddImageAliasBlankIs400(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/images/fake-debian-12-aarch64/aliases",
		url.Values{"alias": {""}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestAddImageAliasDuplicateIs409(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/images/fake-debian-12-aarch64/aliases",
		url.Values{"alias": {"debian/12"}}, false)
	assertStatus(t, res, http.StatusConflict)
}

func TestRemoveImageAliasApplies(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/images/aliases/delete",
		url.Values{"alias": {"debian/12"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.False(t, slices.Contains(localAliases(t, b), "debian/12"))
}

func TestRemoveImageAliasBlankIs400(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/images/aliases/delete", url.Values{"alias": {""}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestRemoveImageAliasGhostIs404(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/images/aliases/delete", url.Values{"alias": {"ghost"}}, false)
	assertStatus(t, res, http.StatusNotFound)
}

func TestUpdateImageAppliesAndReturnsTable(t *testing.T) {
	b := fake.New()
	imgs, err := b.ListLocalImages(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, imgs)
	fp := imgs[0].Fingerprint

	res := formRequest(t, New(b), "/images/"+fp+"/config",
		url.Values{"description": {"edited by test"}, "public": {"on"}}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "edited by test")

	after, err := b.ListLocalImages(t.Context())
	require.NoError(t, err)
	idx := slices.IndexFunc(after, func(i backend.LocalImage) bool { return i.Fingerprint == fp })
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, "edited by test", after[idx].Description)
	assert.True(t, after[idx].Public)
}

func TestUpdateImageGhostIs404(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/images/ghost/config", url.Values{"description": {"x"}}, true)
	assertStatus(t, res, http.StatusNotFound)
}

func TestExportImageDownloads(t *testing.T) {
	b := fake.New()
	imgs, err := b.ListLocalImages(t.Context())
	require.NoError(t, err)
	fp := imgs[0].Fingerprint

	res := request(t, New(b), "GET", "/images/"+fp+"/export", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Header().Get("Content-Disposition"), ".tar")
	assert.Contains(t, res.Body.String(), fp, "fake export blob carries the fingerprint")
}

func TestExportImageGhostIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/images/ghost/export", "", false)
	assertStatus(t, res, http.StatusNotFound)
}

// importImageRequest posts a multipart image upload.
func importImageRequest(t *testing.T, srv *http.Server, content, alias string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if alias != "" {
		require.NoError(t, mw.WriteField("alias", alias))
	}
	fw, err := mw.CreateFormFile("image", "image.tar")
	require.NoError(t, err)
	_, err = fw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/images/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res := httptest.NewRecorder()
	srv.Handler.ServeHTTP(res, req)
	return res
}

func TestImportImageRoundTrip(t *testing.T) {
	b := fake.New()
	imgs, err := b.ListLocalImages(t.Context())
	require.NoError(t, err)
	fp := imgs[0].Fingerprint

	// Export through the handler, re-import with a new alias.
	export := request(t, New(b), "GET", "/images/"+fp+"/export", "", false)
	assertStatus(t, export, http.StatusOK)

	res := importImageRequest(t, New(b), export.Body.String(), "restored")
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/images", res.Header().Get("Location"))
	assert.Contains(t, localAliases(t, b), "restored")
}

func TestImportImageForeignBlobIs400(t *testing.T) {
	res := importImageRequest(t, New(fake.New()), "garbage", "")
	assertStatus(t, res, http.StatusBadRequest)
}

func TestExportSplitImageDownloadsZip(t *testing.T) {
	b := fake.New()
	b.SeedSplitImage("fake-vm-img", "VM image")

	res := request(t, New(b), "GET", "/images/fake-vm-img/export", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Header().Get("Content-Disposition"), ".zip")
	assert.True(t, bytes.HasPrefix(res.Body.Bytes(), []byte("PK\x03\x04")), "split export is a zip")
}

func TestImportSplitImageRoundTrip(t *testing.T) {
	b := fake.New()
	b.SeedSplitImage("fake-vm-img", "VM image")
	srv := New(b)

	export := request(t, srv, "GET", "/images/fake-vm-img/export", "", false)
	assertStatus(t, export, http.StatusOK)

	res := importImageRequest(t, srv, export.Body.String(), "restored-vm")
	assertStatus(t, res, http.StatusSeeOther)
	assert.Contains(t, localAliases(t, b), "restored-vm")
}

func TestUpdateImageLifecycleFieldsAndRefresh(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CopyImage(context.Background(), "ubuntu/24.04"))
	imgs, err := b.ListLocalImages(context.Background())
	require.NoError(t, err)
	var fp string
	for _, img := range imgs {
		if img.HasUpdateSource {
			fp = img.Fingerprint
		}
	}
	require.NotEmpty(t, fp)
	srv := New(b)

	res := formRequest(t, srv, "/images/"+fp+"/config", url.Values{
		"description": {"lts"}, "auto_update": {"on"}, "expires_at": {"2027-03-01T00:00"},
	}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "auto-update")
	assert.Contains(t, res.Body.String(), "expires 2027-03-01")

	res = formRequest(t, srv, "/images/"+fp+"/refresh", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)

	// A published-style image (the seeded one) has no source: refresh 400s.
	res = formRequest(t, srv, "/images/fake-debian-12-aarch64/refresh", url.Values{}, true)
	assertStatus(t, res, http.StatusBadRequest)
}
