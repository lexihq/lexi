package fake

import (
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkZoneCRUDRoundTrip(t *testing.T) {
	f := New()

	zones, err := f.ListNetworkZones(ctx())
	require.NoError(t, err)
	require.Empty(t, zones)

	require.NoError(t, f.CreateNetworkZone(ctx(), "incus.example.org", "forward zone"))
	require.ErrorIs(t, f.CreateNetworkZone(ctx(), "incus.example.org", ""), backend.ErrConflict)
	require.ErrorIs(t, f.CreateNetworkZone(ctx(), "bad zone", ""), backend.ErrInvalid)
	require.ErrorIs(t, f.CreateNetworkZone(ctx(), "", ""), backend.ErrInvalid)

	z, err := f.GetNetworkZone(ctx(), "incus.example.org")
	require.NoError(t, err)
	assert.Equal(t, "forward zone", z.Description)
	require.NotEmpty(t, z.Version)
	_, err = f.GetNetworkZone(ctx(), "ghost.example.org")
	require.ErrorIs(t, err, backend.ErrNotFound)

	// Versioned update: description + config replace; stale version conflicts.
	require.NoError(t, f.UpdateNetworkZone(ctx(), "incus.example.org", "edited", map[string]string{"dns.nameservers": "ns1.example.org"}, z.Version))
	require.ErrorIs(t, f.UpdateNetworkZone(ctx(), "incus.example.org", "stale", nil, z.Version), backend.ErrConflict)
	z, err = f.GetNetworkZone(ctx(), "incus.example.org")
	require.NoError(t, err)
	assert.Equal(t, "edited", z.Description)
	assert.Equal(t, "ns1.example.org", z.Config["dns.nameservers"])

	require.NoError(t, f.DeleteNetworkZone(ctx(), "incus.example.org"))
	require.ErrorIs(t, f.DeleteNetworkZone(ctx(), "incus.example.org"), backend.ErrNotFound)
}

func TestNetworkZoneUsedByNetworksBlocksDelete(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateNetworkZone(ctx(), "fwd.example.org", ""))

	// Point the seeded bridge's forward zone at it: the zone is now in use.
	n, err := f.GetNetwork(ctx(), "incusbr0")
	require.NoError(t, err)
	cfg := n.Config
	cfg["dns.zone.forward"] = "fwd.example.org"
	require.NoError(t, f.UpdateNetwork(ctx(), "incusbr0", n.Description, cfg, ""))

	z, err := f.GetNetworkZone(ctx(), "fwd.example.org")
	require.NoError(t, err)
	assert.Equal(t, []string{"/1.0/networks/incusbr0"}, z.UsedBy)
	require.ErrorIs(t, f.DeleteNetworkZone(ctx(), "fwd.example.org"), backend.ErrConflict)

	// Releasing the reference unblocks deletion.
	delete(cfg, "dns.zone.forward")
	require.NoError(t, f.UpdateNetwork(ctx(), "incusbr0", n.Description, cfg, ""))
	require.NoError(t, f.DeleteNetworkZone(ctx(), "fwd.example.org"))
}

func TestZoneRecordLifecycle(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateNetworkZone(ctx(), "incus.example.org", ""))

	records, err := f.ListZoneRecords(ctx(), "incus.example.org")
	require.NoError(t, err)
	require.Empty(t, records)

	rec := backend.ZoneRecord{
		Name:        "www",
		Description: "web frontend",
		Entries:     []backend.ZoneEntry{{Type: "A", TTL: 300, Value: "10.0.3.10"}},
	}
	require.NoError(t, f.CreateZoneRecord(ctx(), "incus.example.org", rec))
	require.ErrorIs(t, f.CreateZoneRecord(ctx(), "incus.example.org", rec), backend.ErrConflict)
	require.ErrorIs(t, f.CreateZoneRecord(ctx(), "incus.example.org", backend.ZoneRecord{Name: ""}), backend.ErrInvalid)
	require.ErrorIs(t, f.CreateZoneRecord(ctx(), "incus.example.org", backend.ZoneRecord{Name: "x", Entries: []backend.ZoneEntry{{Type: "A"}}}), backend.ErrInvalid)
	require.ErrorIs(t, f.CreateZoneRecord(ctx(), "ghost.example.org", rec), backend.ErrNotFound)

	records, err = f.ListZoneRecords(ctx(), "incus.example.org")
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, rec, records[0])

	require.NoError(t, f.DeleteZoneRecord(ctx(), "incus.example.org", "www"))
	require.ErrorIs(t, f.DeleteZoneRecord(ctx(), "incus.example.org", "www"), backend.ErrNotFound)

	// Records vanish with their zone.
	require.NoError(t, f.CreateZoneRecord(ctx(), "incus.example.org", rec))
	require.NoError(t, f.DeleteNetworkZone(ctx(), "incus.example.org"))
	require.NoError(t, f.CreateNetworkZone(ctx(), "incus.example.org", ""))
	records, err = f.ListZoneRecords(ctx(), "incus.example.org")
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestNetworkZonesAreProjectScopedByFeature(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateNetworkZone(ctx(), "shared.example.org", ""))

	// A project without its own zone feature shares default's zones.
	require.NoError(t, f.CreateProject(ctx(), "plain", "", nil))
	zones, err := f.ListNetworkZones(backend.WithProject(ctx(), "plain"))
	require.NoError(t, err)
	require.Len(t, zones, 1)

	// A project owning features.networks.zones gets its own namespace.
	require.NoError(t, f.CreateProject(ctx(), "zoned", "", map[string]string{"features.networks.zones": "true"}))
	zones, err = f.ListNetworkZones(backend.WithProject(ctx(), "zoned"))
	require.NoError(t, err)
	require.Empty(t, zones)
	require.NoError(t, f.CreateNetworkZone(backend.WithProject(ctx(), "zoned"), "shared.example.org", "same name, own namespace"))
}
