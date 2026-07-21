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

func TestNetworkZonesPageListsZones(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateNetworkZone(t.Context(), backend.NetworkZone{Name: "incus.example.org", Description: "forward zone"}))

	res := request(t, New(b), "GET", "/network-zones", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, "incus.example.org")
	assert.Contains(t, body, "forward zone")
	assert.Contains(t, body, `action="/network-zones"`) // create form
}

func TestCreateNetworkZoneRedirectsToDetail(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/network-zones", url.Values{"name": {"incus.example.org"}, "description": {"d"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/network-zones/incus.example.org", res.Header().Get("Location"))

	res = formRequest(t, New(b), "/network-zones", url.Values{"name": {""}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestNetworkZoneDetailRendersEditorAndRecords(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateNetworkZone(t.Context(), backend.NetworkZone{Name: "incus.example.org", Description: ""}))
	require.NoError(t, b.CreateZoneRecord(t.Context(), "incus.example.org", backend.ZoneRecord{
		Name:    "www",
		Entries: []backend.ZoneEntry{{Type: "A", TTL: 300, Value: "10.0.3.10"}},
	}))

	res := request(t, New(b), "GET", "/network-zones/incus.example.org", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, `action="/network-zones/incus.example.org/config"`)
	assert.Contains(t, body, `action="/network-zones/incus.example.org/records"`)
	assert.Contains(t, body, "www")
	assert.Contains(t, body, "10.0.3.10")
	assert.Contains(t, body, "300")
}

func TestUpdateNetworkZoneConfigVersioned(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateNetworkZone(t.Context(), backend.NetworkZone{Name: "incus.example.org", Description: ""}))
	z, err := b.GetNetworkZone(t.Context(), "incus.example.org")
	require.NoError(t, err)

	form := url.Values{
		"description": {"edited"},
		"key":         {"dns.nameservers"},
		"value":       {"ns1.example.org"},
		"version":     {string(z.Version)},
	}
	res := formRequest(t, New(b), "/network-zones/incus.example.org/config", form, false)
	assertStatus(t, res, http.StatusSeeOther)

	got, err := b.GetNetworkZone(t.Context(), "incus.example.org")
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, "ns1.example.org", got.Config["dns.nameservers"])

	// The stale token (pre-update) now conflicts.
	res = formRequest(t, New(b), "/network-zones/incus.example.org/config", form, false)
	assertStatus(t, res, http.StatusConflict)
}

func TestZoneRecordAddAndDelete(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateNetworkZone(t.Context(), backend.NetworkZone{Name: "incus.example.org", Description: ""}))
	srv := New(b)

	form := url.Values{"record": {"www"}, "type": {"A"}, "value": {"10.0.3.10"}, "ttl": {"300"}, "description": {"web"}}
	res := formRequest(t, srv, "/network-zones/incus.example.org/records", form, false)
	assertStatus(t, res, http.StatusSeeOther)

	records, err := b.ListZoneRecords(t.Context(), "incus.example.org")
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, backend.ZoneRecord{
		Name:        "www",
		Description: "web",
		Entries:     []backend.ZoneEntry{{Type: "A", TTL: 300, Value: "10.0.3.10"}},
	}, records[0])

	// An empty TTL means "zone default" (0).
	form = url.Values{"record": {"db"}, "type": {"CNAME"}, "value": {"www.incus.example.org."}}
	res = formRequest(t, srv, "/network-zones/incus.example.org/records", form, false)
	assertStatus(t, res, http.StatusSeeOther)
	records, err = b.ListZoneRecords(t.Context(), "incus.example.org")
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, uint64(0), records[0].Entries[0].TTL)

	res = formRequest(t, srv, "/network-zones/incus.example.org/records/delete", url.Values{"record": {"www"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	records, err = b.ListZoneRecords(t.Context(), "incus.example.org")
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "db", records[0].Name)
}

func TestZoneRecordValidation(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateNetworkZone(t.Context(), backend.NetworkZone{Name: "incus.example.org", Description: ""}))
	srv := New(b)

	// Missing fields and a malformed TTL are 400s.
	res := formRequest(t, srv, "/network-zones/incus.example.org/records",
		url.Values{"record": {""}, "type": {"A"}, "value": {"10.0.3.10"}}, false)
	assertStatus(t, res, http.StatusBadRequest)
	res = formRequest(t, srv, "/network-zones/incus.example.org/records",
		url.Values{"record": {"www"}, "type": {"A"}, "value": {""}}, false)
	assertStatus(t, res, http.StatusBadRequest)
	res = formRequest(t, srv, "/network-zones/incus.example.org/records",
		url.Values{"record": {"www"}, "type": {"A"}, "value": {"10.0.3.10"}, "ttl": {"soon"}}, false)
	assertStatus(t, res, http.StatusBadRequest)

	records, err := b.ListZoneRecords(t.Context(), "incus.example.org")
	require.NoError(t, err)
	assert.Empty(t, records, "nothing may have been created")
}

func TestDeleteNetworkZoneRedirectsAndGuards(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateNetworkZone(t.Context(), backend.NetworkZone{Name: "incus.example.org", Description: ""}))

	res := formRequest(t, New(b), "/network-zones/incus.example.org/delete", url.Values{}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/network-zones", res.Header().Get("Location"))

	res = formRequest(t, New(b), "/network-zones/ghost.example.org/delete", url.Values{}, false)
	assertStatus(t, res, http.StatusNotFound)
}
