package server

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
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
