package server

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newInstance(t *testing.T) *fake.Fake {
	t.Helper()
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	return b
}

func TestParseSnapshotExpiryIsUTC(t *testing.T) {
	got, err := parseSnapshotExpiry("2026-06-01T03:30")
	require.NoError(t, err)
	// datetime-local carries no offset; we fix it to UTC (not the server zone) so
	// the absolute expiry is deployment-independent.
	assert.Equal(t, time.Date(2026, 6, 1, 3, 30, 0, 0, time.UTC), got)

	zero, err := parseSnapshotExpiry("  ")
	require.NoError(t, err)
	assert.True(t, zero.IsZero())
}

func TestCreateSnapshotStatefulAndExpiry(t *testing.T) {
	b := newInstance(t)
	res := formRequest(t, New(b), "/instances/demo/snapshots",
		url.Values{"snapshot": {"s0"}, "stateful": {"on"}, "expires_at": {"2026-06-01T00:00"}}, true)
	assertStatus(t, res, http.StatusOK)
	snaps, err := b.ListSnapshots(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.True(t, snaps[0].Stateful)
	assert.False(t, snaps[0].ExpiresAt.IsZero())
}

func TestCreateSnapshotInvalidExpiryIs400(t *testing.T) {
	res := formRequest(t, New(newInstance(t)), "/instances/demo/snapshots",
		url.Values{"snapshot": {"s0"}, "expires_at": {"not-a-date"}}, true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestRenameSnapshotReturnsTable(t *testing.T) {
	b := newInstance(t)
	require.NoError(t, b.CreateSnapshot(t.Context(), "demo", "s0", backend.SnapshotOptions{}))
	res := formRequest(t, New(b), "/instances/demo/snapshots/s0/rename",
		url.Values{"new_name": {"s1"}}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "s1")
	snaps, err := b.ListSnapshots(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "s1", snaps[0].Name)
}

func TestRenameSnapshotBlankNameIs400(t *testing.T) {
	b := newInstance(t)
	require.NoError(t, b.CreateSnapshot(t.Context(), "demo", "s0", backend.SnapshotOptions{}))
	res := formRequest(t, New(b), "/instances/demo/snapshots/s0/rename",
		url.Values{"new_name": {"  "}}, true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestUpdateSnapshotExpiryReturnsTable(t *testing.T) {
	b := newInstance(t)
	require.NoError(t, b.CreateSnapshot(t.Context(), "demo", "s0", backend.SnapshotOptions{}))
	res := formRequest(t, New(b), "/instances/demo/snapshots/s0/expiry",
		url.Values{"expires_at": {""}}, true) // empty clears
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "s0")
}
