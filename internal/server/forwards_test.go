package server

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkDetailShowsLeasesAndState(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(context.Background(), backend.CreateOptions{Name: "leasy", Image: "debian/12"}))
	require.NoError(t, b.StartInstance(context.Background(), "leasy"))

	res := request(t, New(b), "GET", "/networks/incusbr0", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "leasy")
	assert.Contains(t, res.Body.String(), "MTU")
}

func TestNetworkForwardHandlers(t *testing.T) {
	b := fake.New()
	srv := New(b)

	// Create.
	res := formRequest(t, srv, "/networks/incusbr0/forwards", url.Values{
		"listen_address": {"192.0.2.20"}, "description": {"web"}, "target_address": {"10.0.3.5"},
	}, false)
	assertStatus(t, res, http.StatusSeeOther)
	fws, err := b.ListNetworkForwards(context.Background(), "incusbr0")
	require.NoError(t, err)
	require.Len(t, fws, 1)

	// Update ports: one filled row, one blank (ignored).
	res = formRequest(t, srv, "/networks/incusbr0/forwards/192.0.2.20/update", url.Values{
		"description":    {"web"},
		"target_address": {"10.0.3.5"},
		"port_protocol":  {"tcp", "tcp"},
		"listen_port":    {"80", ""},
		"port_target":    {"10.0.3.6", ""},
		"target_port":    {"8080", ""},
	}, false)
	assertStatus(t, res, http.StatusSeeOther)
	fws, err = b.ListNetworkForwards(context.Background(), "incusbr0")
	require.NoError(t, err)
	require.Len(t, fws[0].Ports, 1)
	assert.Equal(t, "8080", fws[0].Ports[0].TargetPort)

	// Delete.
	res = formRequest(t, srv, "/networks/incusbr0/forwards/192.0.2.20/delete", url.Values{}, false)
	assertStatus(t, res, http.StatusSeeOther)
	fws, err = b.ListNetworkForwards(context.Background(), "incusbr0")
	require.NoError(t, err)
	assert.Empty(t, fws)

	// Missing listen address on create.
	res = formRequest(t, srv, "/networks/incusbr0/forwards", url.Values{}, false)
	assertStatus(t, res, http.StatusBadRequest)
}
