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

func TestNetworksPageLists(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/networks", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "incusbr0")
}

func TestNetworkDetailRenders(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/networks/incusbr0", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "ipv4.address")
}

func TestNetworkDetailUnknownIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/networks/ghost", "", false)
	assertStatus(t, res, http.StatusNotFound)
}

func TestCreateNetworkAppliesAndRedirects(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/networks",
		url.Values{"name": {"br1"}, "type": {"bridge"}, "description": {"d"},
			"key": {"ipv4.nat", ""}, "value": {"true", ""}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	net, err := b.GetNetwork(t.Context(), "br1")
	require.NoError(t, err)
	assert.Equal(t, "true", net.Config["ipv4.nat"])
}

func TestRenderErrorEmitsToast(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/networks/ghost", "", true)
	assertStatus(t, res, http.StatusNotFound)
	body := res.Body.String()
	assert.Contains(t, body, "data-tui-toast")
	assert.Contains(t, body, "not found")
}

func TestCreateNetworkBlankNameIs400(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/networks",
		url.Values{"name": {"  "}, "type": {"bridge"}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestUpdateNetworkAppliesAndRedirects(t *testing.T) {
	b := fake.New()
	n, err := b.GetNetwork(t.Context(), "incusbr0")
	require.NoError(t, err)

	res := formRequest(t, New(b), "/networks/incusbr0/config",
		url.Values{"description": {"lab bridge"}, "version": {string(n.Version)},
			"key": {"ipv4.nat", ""}, "value": {"false", ""}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/networks/incusbr0", res.Header().Get("Location"))

	got, err := b.GetNetwork(t.Context(), "incusbr0")
	require.NoError(t, err)
	assert.Equal(t, "lab bridge", got.Description)
	assert.Equal(t, map[string]string{"ipv4.nat": "false"}, got.Config)
}

func TestUpdateNetworkStaleVersionIs409(t *testing.T) {
	b := fake.New()
	n, err := b.GetNetwork(t.Context(), "incusbr0")
	require.NoError(t, err)
	require.NoError(t, b.UpdateNetwork(t.Context(), "incusbr0", "racer", nil, n.Version))

	res := formRequest(t, New(b), "/networks/incusbr0/config",
		url.Values{"description": {"stale"}, "version": {string(n.Version)}}, true)
	assertStatus(t, res, http.StatusConflict)
}

func TestUpdateNetworkEditorOnDetailPage(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/networks/incusbr0", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, `action="/networks/incusbr0/config"`)
	assert.Contains(t, body, `name="version"`)
}

func TestUnmanagedNetworkHasNoEditor(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/networks/eth0", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.NotContains(t, res.Body.String(), `action="/networks/eth0/config"`)
}

func TestDeleteNetworkRemovesAndReturnsList(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateNetwork(t.Context(), backend.Network{Name: "br1", Type: "bridge"}))
	res := formRequest(t, New(b), "/networks/br1/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)
	_, err := b.GetNetwork(t.Context(), "br1")
	require.ErrorIs(t, err, backend.ErrNotFound)
}
