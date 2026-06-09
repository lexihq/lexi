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

func TestDeleteNetworkRemovesAndReturnsList(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateNetwork(t.Context(), backend.Network{Name: "br1", Type: "bridge"}))
	res := formRequest(t, New(b), "/networks/br1/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)
	_, err := b.GetNetwork(t.Context(), "br1")
	require.ErrorIs(t, err, backend.ErrNotFound)
}
