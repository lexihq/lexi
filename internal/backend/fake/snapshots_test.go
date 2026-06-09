package fake

import "testing"

func TestSnapshots(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	if err := b.CreateSnapshot(ctx(), "demo", "snap1"); err != nil {
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
	if err := b.CreateSnapshot(ctx(), "ghost", "s"); err == nil {
		t.Fatal("expected error snapshotting missing instance")
	}
	if _, err := b.ListSnapshots(ctx(), "ghost"); err == nil {
		t.Fatal("expected error listing snapshots of missing instance")
	}
}

func TestSnapshotErrors(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	if err := b.CreateSnapshot(ctx(), "demo", "snap1"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Duplicate snapshot name is rejected.
	if err := b.CreateSnapshot(ctx(), "demo", "snap1"); err == nil {
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
