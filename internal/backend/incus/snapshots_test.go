package incus

import (
	"net/http"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListSnapshotsMapsStructuredStatus(t *testing.T) {
	b := &incusBackend{
		srv: &instanceServerStub{
			snapshotErr: api.StatusErrorf(http.StatusNotFound, "missing"),
		},
	}

	_, err := b.ListSnapshots(t.Context(), "ghost")

	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestCreateSnapshotSendsStatefulAndExpiry(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	exp := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, b.CreateSnapshot(t.Context(), "demo", "snap0", backend.SnapshotOptions{Stateful: true, ExpiresAt: exp}))
	require.NotNil(t, s.snapshotPost)
	assert.Equal(t, "snap0", s.snapshotPost.Name)
	assert.True(t, s.snapshotPost.Stateful)
	require.NotNil(t, s.snapshotPost.ExpiresAt)
	assert.Equal(t, exp, *s.snapshotPost.ExpiresAt)
}

func TestCreateSnapshotOmitsZeroExpiry(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.CreateSnapshot(t.Context(), "demo", "snap0", backend.SnapshotOptions{}))
	require.NotNil(t, s.snapshotPost)
	assert.Nil(t, s.snapshotPost.ExpiresAt)
}

func TestRenameSnapshotCallsThrough(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.RenameSnapshot(t.Context(), "demo", "snap0", "snap1"))
	assert.Equal(t, [3]string{"demo", "snap0", "snap1"}, s.renamedSnap)
}

func TestUpdateSnapshotExpiryPutsExpiry(t *testing.T) {
	exp := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	s := &instanceServerStub{snap: &api.InstanceSnapshot{}}
	b := &incusBackend{srv: s}
	require.NoError(t, b.UpdateSnapshotExpiry(t.Context(), "demo", "snap0", exp))
	require.NotNil(t, s.snapExpiry)
	assert.Equal(t, exp, s.snapExpiry.ExpiresAt)
}

func TestListSnapshotsMapsExpiresAt(t *testing.T) {
	exp := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	snap := api.InstanceSnapshot{}
	snap.Name = "snap0"
	snap.ExpiresAt = exp
	b := &incusBackend{srv: &instanceServerStub{snapshotSnaps: []api.InstanceSnapshot{snap}}}
	snaps, err := b.ListSnapshots(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, exp, snaps[0].ExpiresAt)
}

func TestGetSnapshotScheduleReadsConfig(t *testing.T) {
	inst := &api.Instance{}
	inst.Config = map[string]string{"snapshots.schedule": "@daily", "snapshots.expiry": "2w", "snapshots.pattern": "snap%d"}
	b := &incusBackend{srv: &instanceServerStub{instance: inst}}
	s, err := b.GetSnapshotSchedule(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "@daily", s.Schedule)
	assert.Equal(t, "2w", s.Expiry)
	assert.Equal(t, "snap%d", s.Pattern)
}

func TestSetSnapshotScheduleWritesKeys(t *testing.T) {
	inst := &api.Instance{}
	inst.Config = map[string]string{"snapshots.expiry": "1w"} // stale value to be cleared
	s := &instanceServerStub{instance: inst}
	b := &incusBackend{srv: s}
	require.NoError(t, b.SetSnapshotSchedule(t.Context(), "demo", backend.SnapshotSchedule{Schedule: "@daily", Pattern: "snap%d"}))
	require.NotNil(t, s.updatedPut)
	assert.Equal(t, "@daily", s.updatedPut.Config["snapshots.schedule"])
	assert.Equal(t, "snap%d", s.updatedPut.Config["snapshots.pattern"])
	_, hasExpiry := s.updatedPut.Config["snapshots.expiry"]
	assert.False(t, hasExpiry, "empty expiry should be cleared")
}

func TestSetSnapshotScheduleInitsNilConfig(t *testing.T) {
	// Incus can return an instance with a nil local Config; writing must not panic.
	s := &instanceServerStub{instance: &api.Instance{}} // Config is nil
	b := &incusBackend{srv: s}
	require.NoError(t, b.SetSnapshotSchedule(t.Context(), "demo", backend.SnapshotSchedule{Schedule: "@daily"}))
	require.NotNil(t, s.updatedPut)
	assert.Equal(t, "@daily", s.updatedPut.Config["snapshots.schedule"])
}

func TestManagedConfigKeyExcludesSnapshotSchedule(t *testing.T) {
	assert.True(t, managedConfigKey("snapshots.schedule"))
	assert.True(t, managedConfigKey("snapshots.expiry"))
	assert.True(t, managedConfigKey("snapshots.pattern"))
	assert.False(t, managedConfigKey("boot.autostart"))
}
