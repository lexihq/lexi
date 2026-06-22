package incus

import (
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetNetworkZoneCarriesEtagAsVersion(t *testing.T) {
	z := &api.NetworkZone{Name: "incus.example.org"}
	z.Description = "forward zone"
	z.Config = map[string]string{"dns.nameservers": "ns1.example.org"}
	z.UsedBy = []string{"/1.0/networks/incusbr0"}
	b := &incusBackend{srv: &instanceServerStub{zone: z}}

	got, err := b.GetNetworkZone(t.Context(), "incus.example.org")
	require.NoError(t, err)
	assert.Equal(t, "zone-etag", got.Version)
	assert.Equal(t, "forward zone", got.Description)
	assert.Equal(t, "ns1.example.org", got.Config["dns.nameservers"])
	assert.Equal(t, []string{"/1.0/networks/incusbr0"}, got.UsedBy)
}

func TestCreateNetworkZoneSendsPost(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.CreateNetworkZone(t.Context(), backend.NetworkZone{Name: "incus.example.org", Description: "d"}))
	require.NotNil(t, s.createdZone)
	assert.Equal(t, "incus.example.org", s.createdZone.Name)
	assert.Equal(t, "d", s.createdZone.Description)
}

func TestUpdateNetworkZoneSendsEtag(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.UpdateNetworkZone(t.Context(), "incus.example.org", "edited", map[string]string{"k": "v"}, "etag-1"))
	require.NotNil(t, s.updatedZone)
	assert.Equal(t, "edited", s.updatedZone.Description)
	assert.Equal(t, api.ConfigMap{"k": "v"}, s.updatedZone.Config)
	assert.Equal(t, "etag-1", s.zoneEtag)
}

func TestDeleteNetworkZoneInUseIsConflict(t *testing.T) {
	z := &api.NetworkZone{Name: "incus.example.org"}
	z.UsedBy = []string{"/1.0/networks/incusbr0"}
	b := &incusBackend{srv: &instanceServerStub{zone: z}}

	err := b.DeleteNetworkZone(t.Context(), "incus.example.org")
	require.ErrorIs(t, err, backend.ErrConflict)
}

func TestZoneRecordsMapAndSort(t *testing.T) {
	s := &instanceServerStub{zoneRecords: []api.NetworkZoneRecord{
		{Name: "www", NetworkZoneRecordPut: api.NetworkZoneRecordPut{
			Description: "web",
			Entries:     []api.NetworkZoneRecordEntry{{Type: "A", TTL: 300, Value: "10.0.3.10"}},
		}},
		{Name: "db", NetworkZoneRecordPut: api.NetworkZoneRecordPut{
			Entries: []api.NetworkZoneRecordEntry{{Type: "CNAME", Value: "www.incus.example.org."}},
		}},
	}}
	b := &incusBackend{srv: s}

	got, err := b.ListZoneRecords(t.Context(), "incus.example.org")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "db", got[0].Name, "records must sort by name")
	assert.Equal(t, backend.ZoneRecord{
		Name:        "www",
		Description: "web",
		Entries:     []backend.ZoneEntry{{Type: "A", TTL: 300, Value: "10.0.3.10"}},
	}, got[1])
}

func TestCreateZoneRecordSendsEntries(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.CreateZoneRecord(t.Context(), "incus.example.org", backend.ZoneRecord{
		Name:    "www",
		Entries: []backend.ZoneEntry{{Type: "A", TTL: 60, Value: "10.0.3.10"}},
	}))
	require.NotNil(t, s.createdZoneRecord)
	assert.Equal(t, "incus.example.org", s.zoneRecordZone)
	assert.Equal(t, "www", s.createdZoneRecord.Name)
	require.Len(t, s.createdZoneRecord.Entries, 1)
	assert.Equal(t, api.NetworkZoneRecordEntry{Type: "A", TTL: 60, Value: "10.0.3.10"}, s.createdZoneRecord.Entries[0])
}

func TestDeleteZoneRecordPassesNames(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.DeleteZoneRecord(t.Context(), "incus.example.org", "www"))
	assert.Equal(t, "incus.example.org", s.zoneRecordZone)
	assert.Equal(t, "www", s.deletedZoneRecord)
}
