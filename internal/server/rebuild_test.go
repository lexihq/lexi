package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRebuildFormRendersImagePicker(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), http.MethodGet, "/instances/demo/rebuild", "", false)

	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "Rebuild demo")
	assert.Contains(t, res.Body.String(), `id="image-results"`)
}

func TestRebuildFormGhostIs404(t *testing.T) {
	res := request(t, New(fake.New()), http.MethodGet, "/instances/ghost/rebuild", "", false)
	assertStatus(t, res, http.StatusNotFound)
}

func TestRebuildAppliesAndRedirects(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	form := url.Values{"image": {"fake-alpine-edge-aarch64"}}
	res := formRequest(t, New(b), "/instances/demo/rebuild", form, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/instances/demo", res.Header().Get("Location"))

	inst, err := b.GetInstance(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "alpine/edge", inst.Image)
}

func TestRebuildRunningInstanceIs400(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12", Start: true}))

	form := url.Values{"image": {"fake-alpine-edge-aarch64"}}
	res := formRequest(t, New(b), "/instances/demo/rebuild", form, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestRebuildUnknownImageIs400(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := formRequest(t, New(b), "/instances/demo/rebuild", url.Values{"image": {"no-such"}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestRebuildMissingImageIs400(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := formRequest(t, New(b), "/instances/demo/rebuild", url.Values{}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestRebuildGhostInstanceIs404(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/instances/ghost/rebuild",
		url.Values{"image": {"fake-alpine-edge-aarch64"}}, false)
	assertStatus(t, res, http.StatusNotFound)
}
