package fake

import (
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshots(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	if err := b.CreateSnapshot(ctx(), "demo", "snap1", backend.SnapshotOptions{}); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	snaps, err := b.ListSnapshots(ctx(), "demo")
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(snaps) != 1 || snaps[0].Name != "snap1" {
		t.Fatalf("unexpected snapshots: %+v", snaps)
	}

	// Snapshot count surfaces on the instance.
	inst, err := b.GetInstance(ctx(), "demo")
	if err != nil {
		t.Fatalf("get instance after snapshot: %v", err)
	}
	if inst.Snapshots != 1 {
		t.Fatalf("want snapshot count 1, got %d", inst.Snapshots)
	}

	if err := b.RestoreSnapshot(ctx(), "demo", "snap1"); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if err := b.DeleteSnapshot(ctx(), "demo", "snap1"); err != nil {
		t.Fatalf("delete snapshot: %v", err)
	}
	snaps, err = b.ListSnapshots(ctx(), "demo")
	if err != nil {
		t.Fatalf("list snapshots after delete: %v", err)
	}
	if len(snaps) != 0 {
		t.Fatalf("want 0 snapshots after delete, got %d", len(snaps))
	}

	// Snapshot operations on a missing instance error.
	if err := b.CreateSnapshot(ctx(), "ghost", "s", backend.SnapshotOptions{}); err == nil {
		t.Fatal("expected error snapshotting missing instance")
	}
	if _, err := b.ListSnapshots(ctx(), "ghost"); err == nil {
		t.Fatal("expected error listing snapshots of missing instance")
	}
}

func TestSnapshotErrors(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	if err := b.CreateSnapshot(ctx(), "demo", "snap1", backend.SnapshotOptions{}); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Duplicate snapshot name is rejected.
	if err := b.CreateSnapshot(ctx(), "demo", "snap1", backend.SnapshotOptions{}); err == nil {
		t.Fatal("expected error creating duplicate snapshot")
	}

	// Restore/delete of a non-existent snapshot on an existing instance error.
	if err := b.RestoreSnapshot(ctx(), "demo", "nope"); err == nil {
		t.Fatal("expected error restoring missing snapshot")
	}
	if err := b.DeleteSnapshot(ctx(), "demo", "nope"); err == nil {
		t.Fatal("expected error deleting missing snapshot")
	}
}

func TestSnapshotExtras(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	exp := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, b.CreateSnapshot(ctx(), "demo", "snap0", backend.SnapshotOptions{Stateful: true, ExpiresAt: exp}))
	snaps, err := b.ListSnapshots(ctx(), "demo")
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.True(t, snaps[0].Stateful)
	assert.Equal(t, exp, snaps[0].ExpiresAt)

	// Rename: collision + missing + success.
	require.NoError(t, b.CreateSnapshot(ctx(), "demo", "other", backend.SnapshotOptions{}))
	require.ErrorIs(t, b.RenameSnapshot(ctx(), "demo", "snap0", "other"), backend.ErrConflict)
	require.ErrorIs(t, b.RenameSnapshot(ctx(), "demo", "ghost", "x"), backend.ErrNotFound)
	require.NoError(t, b.RenameSnapshot(ctx(), "demo", "snap0", "snap1"))

	// Edit expiry (clear).
	require.NoError(t, b.UpdateSnapshotExpiry(ctx(), "demo", "snap1", time.Time{}))
	snaps, err = b.ListSnapshots(ctx(), "demo")
	require.NoError(t, err)
	for _, s := range snaps {
		if s.Name == "snap1" {
			assert.True(t, s.ExpiresAt.IsZero())
		}
	}
	require.ErrorIs(t, b.UpdateSnapshotExpiry(ctx(), "demo", "ghost", exp), backend.ErrNotFound)
}
