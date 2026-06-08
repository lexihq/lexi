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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestListImages (read path) asserts the full simplestreams catalog resolves —
// far larger than the v1 curated set — with filterable fields populated.
func TestListImages(t *testing.T) {
	imgs, err := newBackend(t).ListImages(context.Background())
	require.NoError(t, err)
	require.Greater(t, len(imgs), 10, "expected the full catalog, not a curated subset")

	distros := map[string]bool{}
	var foundTest bool
	for _, i := range imgs {
		distros[i.Distribution] = true
		if i.Alias == testImage {
			foundTest = true
		}
	}
	assert.True(t, foundTest, "test image %q should be in the catalog", testImage)
	assert.Greater(t, len(distros), 1, "catalog should span multiple distributions")
	t.Logf("resolved %d images across %d distributions", len(imgs), len(distros))
}

// TestUpdateLimitsRoundTrip sets and clears limits on a throwaway container and
// reads them back through the expanded config.
func TestUpdateLimitsRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("limits")
	t.Cleanup(func() { _ = b.DeleteInstance(context.Background(), name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))
	require.NoError(t, b.UpdateLimits(ctx, name, backend.Limits{CPU: "2", Memory: "256MiB"}))

	inst, err := b.GetInstance(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "2", inst.LimitsCPU)
	assert.Equal(t, "256MiB", inst.LimitsMemory)

	// Empty limits clear the keys.
	require.NoError(t, b.UpdateLimits(ctx, name, backend.Limits{}))
	inst, err = b.GetInstance(ctx, name)
	require.NoError(t, err)
	assert.Empty(t, inst.LimitsCPU)
	assert.Empty(t, inst.LimitsMemory)
}

// TestMetricsReportsUsage starts a throwaway container and reads its live
// metrics back, asserting memory usage is populated once it is running.
func TestMetricsReportsUsage(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("metrics")
	t.Cleanup(func() { _ = b.DeleteInstance(context.Background(), name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage, Start: true}))

	m, err := b.Metrics(ctx, name)
	require.NoError(t, err)
	assert.Greater(t, m.MemoryUsage, int64(0), "running instance should report memory usage")
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
