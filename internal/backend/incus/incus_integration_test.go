//go:build integration

// Integration tests run against a real Incus daemon via the current `incus` CLI
// remote (see Makefile target test-integration). They are excluded from the
// default `go test ./...` build.
package incus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
)

// testImage is small and fast to pull, keeping integration runs cheap.
const testImage = "alpine/edge"

func newBackend(t *testing.T) *incusBackend {
	t.Helper()
	b, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("lxcon-it-%s-%d", prefix, time.Now().UnixNano()%1_000_000)
}

func listed(list []backend.Instance, name string) bool {
	for _, i := range list {
		if i.Name == name {
			return true
		}
	}
	return false
}

func TestConnect(t *testing.T) {
	b, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	caps := b.Capabilities()
	if caps.Tier != backend.TierIncus {
		t.Fatalf("want tier %q, got %q", backend.TierIncus, caps.Tier)
	}
	if caps.ServerInfo == "" {
		t.Fatal("ServerInfo should report the server version")
	}
	if !caps.Snapshots || !caps.Clone {
		t.Fatalf("incus tier should advertise snapshots and clone: %+v", caps)
	}
	t.Logf("connected: %s", caps.ServerInfo)
}

// TestListImages (read path) asserts the curated alias set resolves against the
// real images remote.
func TestListImages(t *testing.T) {
	imgs, err := newBackend(t).ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	if len(imgs) == 0 {
		t.Fatal("expected curated images, got none")
	}
	var found bool
	for _, i := range imgs {
		if i.Alias == testImage {
			found = true
		}
	}
	if !found {
		t.Fatalf("curated image %q not resolved: %+v", testImage, imgs)
	}
	t.Logf("resolved %d curated images", len(imgs))
}

// TestRoundTripLifecycle covers the write paths: create+start, read back as
// Running, stop, delete (and confirm it leaves the list).
func TestRoundTripLifecycle(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("life")
	t.Cleanup(func() { _ = b.DeleteInstance(context.Background(), name) })

	if err := b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage, Start: true}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if inst, err := b.GetInstance(ctx, name); err != nil || inst.Status != "Running" {
		t.Fatalf("want Running after create+start: inst=%+v err=%v", inst, err)
	}
	if err := b.StopInstance(ctx, name); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if inst, _ := b.GetInstance(ctx, name); inst.Status != "Stopped" {
		t.Fatalf("want Stopped after stop, got %q", inst.Status)
	}
	if err := b.DeleteInstance(ctx, name); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if list, _ := b.ListInstances(ctx); listed(list, name) {
		t.Fatal("instance still listed after delete")
	}
}

// TestRoundTripFull is the brainstorm's spike: New -> Start -> Snapshot ->
// Clone -> Restore -> Delete (clone + original), asserted via the read paths.
func TestRoundTripFull(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("full")
	clone := name + "-copy"
	t.Cleanup(func() {
		_ = b.DeleteInstance(context.Background(), clone)
		_ = b.DeleteInstance(context.Background(), name)
	})

	if err := b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage, Start: true}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if inst, _ := b.GetInstance(ctx, name); inst.Status != "Running" {
		t.Fatalf("want Running, got %q", inst.Status)
	}

	if err := b.CreateSnapshot(ctx, name, "snap0"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snaps, err := b.ListSnapshots(ctx, name); err != nil || len(snaps) != 1 || snaps[0].Name != "snap0" {
		t.Fatalf("snapshots: %+v err=%v", snaps, err)
	}

	// Stop before clone/restore to avoid running-state restrictions.
	if err := b.StopInstance(ctx, name); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if err := b.CloneInstance(ctx, name, clone); err != nil {
		t.Fatalf("clone: %v", err)
	}
	list, _ := b.ListInstances(ctx)
	if !listed(list, name) || !listed(list, clone) {
		t.Fatalf("want both %q and %q listed: %+v", name, clone, list)
	}

	if err := b.RestoreSnapshot(ctx, name, "snap0"); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if err := b.DeleteSnapshot(ctx, name, "snap0"); err != nil {
		t.Fatalf("delete snapshot: %v", err)
	}
	if snaps, _ := b.ListSnapshots(ctx, name); len(snaps) != 0 {
		t.Fatalf("want 0 snapshots after delete, got %d", len(snaps))
	}

	if err := b.DeleteInstance(ctx, clone); err != nil {
		t.Fatalf("delete clone: %v", err)
	}
	if err := b.DeleteInstance(ctx, name); err != nil {
		t.Fatalf("delete original: %v", err)
	}
	if list, _ := b.ListInstances(ctx); listed(list, name) || listed(list, clone) {
		t.Fatal("instances still listed after delete")
	}
}
