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
		url.Values{"description": {"edited"}, "version": {string(p.Version)},
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
		url.Values{"description": {"stale"}, "version": {string(p.Version)}}, true)
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

func TestRenameProfileRedirectsToNewName(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateProfile(t.Context(), "web", "d"))
	res := formRequest(t, New(b), "/profiles/web/rename", url.Values{"new_name": {"frontend"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/profiles/frontend", res.Header().Get("Location"))
	_, err := b.GetProfile(t.Context(), "frontend")
	require.NoError(t, err)
}

func TestRenameDefaultProfileIs400(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/profiles/default/rename", url.Values{"new_name": {"x"}}, true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestAddProfileDeviceReturnsSection(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateProfile(t.Context(), "web", ""))
	res := formRequest(t, New(b), "/profiles/web/devices",
		url.Values{"type": {"nic"}, "device": {"eth0"}, "network": {"lxdbr0"}}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), `id="profile-devices"`)
	assert.Contains(t, res.Body.String(), "eth0")

	p, err := b.GetProfile(t.Context(), "web")
	require.NoError(t, err)
	assert.Equal(t, "lxdbr0", p.Devices["eth0"]["network"])
}

func TestUpdateProfileDeviceMergesAndPreservesUnknownKeys(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateProfile(t.Context(), "web", ""))
	require.NoError(t, b.AddProfileDevice(t.Context(), "web", "eth0", map[string]string{"type": "nic", "network": "lxdbr0", "x.custom": "keep"}))
	p, err := b.GetProfile(t.Context(), "web")
	require.NoError(t, err)

	res := formRequest(t, New(b), "/profiles/web/devices/eth0",
		url.Values{"version": {string(p.Version)}, "network": {"br1"}}, true)
	assertStatus(t, res, http.StatusOK)

	got, err := b.GetProfile(t.Context(), "web")
	require.NoError(t, err)
	assert.Equal(t, "br1", got.Devices["eth0"]["network"])
	assert.Equal(t, "keep", got.Devices["eth0"]["x.custom"], "unknown keys preserved")
	assert.Equal(t, "nic", got.Devices["eth0"]["type"], "type preserved")
}

func TestUpdateProfileDeviceMissingVersionIs400(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateProfile(t.Context(), "web", ""))
	require.NoError(t, b.AddProfileDevice(t.Context(), "web", "eth0", map[string]string{"type": "nic"}))
	res := formRequest(t, New(b), "/profiles/web/devices/eth0", url.Values{"network": {"br1"}}, true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestRemoveProfileDeviceReturnsSection(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateProfile(t.Context(), "web", ""))
	require.NoError(t, b.AddProfileDevice(t.Context(), "web", "eth0", map[string]string{"type": "nic"}))
	res := formRequest(t, New(b), "/profiles/web/devices/eth0/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)

	p, err := b.GetProfile(t.Context(), "web")
	require.NoError(t, err)
	assert.NotContains(t, p.Devices, "eth0")
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
