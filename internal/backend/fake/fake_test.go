package fake

import (
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/require"
)

func TestSentinelErrors(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	// Missing instance → ErrNotFound.
	_, err := b.GetInstance(ctx(), "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)

	// Duplicate name → ErrConflict.
	err = b.CreateInstance(ctx(), backend.CreateOptions{Name: "demo", Image: "x"})
	require.ErrorIs(t, err, backend.ErrConflict)

	// Missing snapshot on an existing instance → ErrNotFound.
	require.ErrorIs(t, b.RestoreSnapshot(ctx(), "demo", "nope"), backend.ErrNotFound)

	// Duplicate snapshot → ErrConflict.
	require.NoError(t, b.CreateSnapshot(ctx(), "demo", "s1", backend.SnapshotOptions{}))
	require.ErrorIs(t, b.CreateSnapshot(ctx(), "demo", "s1", backend.SnapshotOptions{}), backend.ErrConflict)
}

func TestCapabilitiesAdvertisesSnapshotAndClone(t *testing.T) {
	caps := New().Capabilities()
	if !caps.Snapshots || !caps.Clone {
		t.Fatalf("fake should support snapshots and clone, got %+v", caps)
	}
	if !caps.Pause {
		t.Fatalf("fake should support pause/resume, got %+v", caps)
	}
	if !caps.Config {
		t.Fatalf("fake should support config editing, got %+v", caps)
	}
	if caps.ServerInfo == "" {
		t.Fatal("ServerInfo should be set")
	}
}
