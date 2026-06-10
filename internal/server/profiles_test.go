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

func TestCreateProfileAppliesAndRedirects(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/profiles",
		url.Values{"name": {"web"}, "description": {"web servers"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/profiles/web", res.Header().Get("Location"))

	p, err := b.GetProfile(t.Context(), "web")
	require.NoError(t, err)
	assert.Equal(t, "web servers", p.Description)
}

func TestCreateProfileBlankNameIs400(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/profiles", url.Values{"name": {"  "}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestUpdateProfileAppliesAndRedirects(t *testing.T) {
	b := fake.New()
	p, err := b.GetProfile(t.Context(), "default")
	require.NoError(t, err)

	res := formRequest(t, New(b), "/profiles/default/config",
		url.Values{"description": {"edited"}, "version": {p.Version},
			"key": {"limits.cpu", ""}, "value": {"2", ""}}, false)
	assertStatus(t, res, http.StatusSeeOther)

	got, err := b.GetProfile(t.Context(), "default")
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, map[string]string{"limits.cpu": "2"}, got.Config)
	assert.Equal(t, p.Devices, got.Devices, "devices must survive the editor")
}

func TestUpdateProfileStaleVersionIs409(t *testing.T) {
	b := fake.New()
	p, err := b.GetProfile(t.Context(), "default")
	require.NoError(t, err)
	require.NoError(t, b.UpdateProfile(t.Context(), "default", "racer", nil, p.Version))

	res := formRequest(t, New(b), "/profiles/default/config",
		url.Values{"description": {"stale"}, "version": {p.Version}}, true)
	assertStatus(t, res, http.StatusConflict)
}

func TestDeleteProfileRemovesAndRedirects(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/profiles/gpu/delete", url.Values{}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/profiles", res.Header().Get("Location"))
	_, err := b.GetProfile(t.Context(), "gpu")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestDeleteDefaultProfileIs400(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/profiles/default/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestDeleteInUseProfileIs409(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	require.NoError(t, b.SetInstanceProfiles(t.Context(), "demo", []string{"default", "gpu"}))
	res := formRequest(t, New(b), "/profiles/gpu/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusConflict)
}

func TestProfileDetailHasEditorAndDelete(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/profiles/gpu", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, `action="/profiles/gpu/config"`)
	assert.Contains(t, body, `name="version"`)
	assert.Contains(t, body, `action="/profiles/gpu/delete"`)
}

func TestDefaultProfileDetailHasNoDelete(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/profiles/default", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.NotContains(t, res.Body.String(), `action="/profiles/default/delete"`)
}

func TestProfilesPageHasCreateForm(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/profiles", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), `action="/profiles"`)
}
