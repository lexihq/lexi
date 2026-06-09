package incus

import (
	"net/http"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
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
