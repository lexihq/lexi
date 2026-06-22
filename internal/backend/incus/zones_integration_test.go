//go:build integration

package incus

import (
	"context"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNetworkZoneRoundTrip creates a throwaway zone, edits it (versioned),
// round-trips a record, and deletes everything.
func TestNetworkZoneRoundTrip(t *testing.T) {
	b := newBackend(t)
	if !b.Capabilities(context.Background()).NetworkZones {
		t.Skip("daemon lacks the network_dns extension")
	}
	ctx := context.Background()
	name := uniqueName("lxzone") + ".example.org"
	t.Cleanup(func() { _ = b.DeleteNetworkZone(ctx, name) })

	require.NoError(t, b.CreateNetworkZone(ctx, backend.NetworkZone{Name: name, Description: "made by test"}))
	require.ErrorIs(t, b.CreateNetworkZone(ctx, backend.NetworkZone{Name: name, Description: ""}), backend.ErrConflict)

	z, err := b.GetNetworkZone(ctx, name)
	require.NoError(t, err)
	require.NotEmpty(t, z.Version)
	assert.Equal(t, "made by test", z.Description)

	// Versioned update: stale etag conflicts after a successful write.
	require.NoError(t, b.UpdateNetworkZone(ctx, name, "edited", map[string]string{"user.lexi": "yes"}, z.Version))
	require.ErrorIs(t, b.UpdateNetworkZone(ctx, name, "stale", nil, z.Version), backend.ErrConflict)
	z, err = b.GetNetworkZone(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "edited", z.Description)
	assert.Equal(t, "yes", z.Config["user.lexi"])

	// Record lifecycle.
	rec := backend.ZoneRecord{
		Name:        "www",
		Description: "web",
		Entries:     []backend.ZoneEntry{{Type: "A", TTL: 300, Value: "10.0.3.10"}},
	}
	require.NoError(t, b.CreateZoneRecord(ctx, name, rec))
	require.ErrorIs(t, b.CreateZoneRecord(ctx, name, rec), backend.ErrConflict)
	records, err := b.ListZoneRecords(ctx, name)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, rec, records[0])
	require.NoError(t, b.DeleteZoneRecord(ctx, name, "www"))

	require.NoError(t, b.DeleteNetworkZone(ctx, name))
	_, err = b.GetNetworkZone(ctx, name)
	require.ErrorIs(t, err, backend.ErrNotFound)
}
