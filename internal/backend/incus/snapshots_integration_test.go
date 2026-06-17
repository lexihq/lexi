//go:build integration

package incus

import (
	"context"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotExtrasRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("snapx")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	// Stateless snapshot with an expiry.
	exp := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, b.CreateSnapshot(ctx, name, "snap0", backend.SnapshotOptions{ExpiresAt: exp}))
	snaps, err := b.ListSnapshots(ctx, name)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "snap0", snaps[0].Name)
	assert.False(t, snaps[0].ExpiresAt.IsZero())

	// Rename.
	require.NoError(t, b.RenameSnapshot(ctx, name, "snap0", "snap1"))
	snaps, err = b.ListSnapshots(ctx, name)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "snap1", snaps[0].Name)

	// Clear expiry, then delete.
	require.NoError(t, b.UpdateSnapshotExpiry(ctx, name, "snap1", time.Time{}))
	require.NoError(t, b.DeleteSnapshot(ctx, name, "snap1"))

	// Stateful create needs a running instance + CRIU; attempt and skip the
	// assertion (not the test) if the host doesn't support it.
	require.NoError(t, b.StartInstance(ctx, name))
	if err := b.CreateSnapshot(ctx, name, "stateful0", backend.SnapshotOptions{Stateful: true}); err != nil {
		t.Logf("stateful snapshot unsupported on this host (ok): %v", err)
		return
	}
	snaps, err = b.ListSnapshots(ctx, name)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.True(t, snaps[0].Stateful)
	require.NoError(t, b.DeleteSnapshot(ctx, name, "stateful0"))
}

func TestSnapshotScheduleRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("sched")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	require.NoError(t, b.SetSnapshotSchedule(ctx, name, backend.SnapshotSchedule{Schedule: "@daily", Expiry: "2w", Pattern: "snap%d"}))
	s, err := b.GetSnapshotSchedule(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "@daily", s.Schedule)
	assert.Equal(t, "2w", s.Expiry)
	assert.Equal(t, "snap%d", s.Pattern)

	// Clearing fields removes the keys.
	require.NoError(t, b.SetSnapshotSchedule(ctx, name, backend.SnapshotSchedule{Schedule: "@daily"}))
	s, err = b.GetSnapshotSchedule(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "@daily", s.Schedule)
	assert.Empty(t, s.Expiry)
	assert.Empty(t, s.Pattern)
}
