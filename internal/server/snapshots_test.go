package server

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
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

func TestSnapshotScheduleFormRenders(t *testing.T) {
	res := request(t, New(newInstance(t)), "GET", "/instances/demo/snapshots/schedule", "", true)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, `name="schedule"`)
	assert.Contains(t, body, `name="expiry"`)
	assert.Contains(t, body, `name="pattern"`)
}

func TestSetSnapshotScheduleAppliesAndReturnsForm(t *testing.T) {
	b := newInstance(t)
	res := formRequest(t, New(b), "/instances/demo/snapshots/schedule",
		url.Values{"schedule": {"@daily"}, "expiry": {"2w"}, "pattern": {"snap%d"}}, true)
	assertStatus(t, res, http.StatusOK)
	s, err := b.GetSnapshotSchedule(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "@daily", s.Schedule)
	assert.Equal(t, "2w", s.Expiry)
	assert.Equal(t, "snap%d", s.Pattern)
	assert.Contains(t, res.Body.String(), "@daily")
}

func TestSnapshotsTabIncludesScheduleLazyLoad(t *testing.T) {
	res := request(t, New(newInstance(t)), "GET", "/instances/demo?tab=snapshots", "", true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), `/instances/demo/snapshots/schedule`)
}

// Expiry accepts operator-style durations as well as the absolute
// datetime-local form. Units follow the daemon's snapshots.expiry grammar:
// M is minutes, m is MONTHS.
func TestParseSnapshotExpiryDurations(t *testing.T) {
	for raw, want := range map[string]time.Duration{
		"30M": 30 * time.Minute,
		"12h": 12 * time.Hour,
		"12H": 12 * time.Hour,
		"7d":  7 * 24 * time.Hour,
		"2w":  14 * 24 * time.Hour,
	} {
		got, err := parseSnapshotExpiry(raw)
		require.NoError(t, err, raw)
		assert.WithinDuration(t, time.Now().UTC().Add(want), got, time.Minute, raw)
	}

	// Stray whitespace (mobile keyboards, paste) must not 400.
	got, err := parseSnapshotExpiry(" 2w ")
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().UTC().Add(14*24*time.Hour), got, time.Minute)

	// Months and years follow the calendar, not a fixed duration.
	got, err = parseSnapshotExpiry("6m")
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().UTC().AddDate(0, 6, 0), got, time.Minute)
	got, err = parseSnapshotExpiry("1y")
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().UTC().AddDate(1, 0, 0), got, time.Minute)

	for _, raw := range []string{
		"2x",                   // unknown unit
		"0d",                   // zero duration = expiry now = instant pruning
		"9223372036854775807M", // would overflow into a past timestamp
	} {
		_, err := parseSnapshotExpiry(raw)
		require.ErrorIs(t, err, backend.ErrInvalid, raw)
	}
}
