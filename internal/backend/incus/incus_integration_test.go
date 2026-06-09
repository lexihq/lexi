//go:build integration

// Integration tests run against a real Incus daemon via the current `incus` CLI
// remote (see Makefile target test-integration). They are excluded from the
// default `go test ./...` build.
package incus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

func cleanupInstance(t *testing.T, b *incusBackend, name string) {
	t.Helper()
	if err := b.DeleteInstance(context.Background(), name); err != nil && !errors.Is(err, backend.ErrNotFound) {
		t.Errorf("cleanup instance %q: %v", name, err)
	}
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
	t.Cleanup(func() { cleanupInstance(t, b, name) })

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
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage, Start: true}))

	m, err := b.Metrics(ctx, name)
	require.NoError(t, err)
	assert.Greater(t, m.MemoryUsage, int64(0), "running instance should report memory usage")
}

// TestExportInstanceProducesTarball exports a throwaway instance to a temp file
// and asserts it is a non-empty gzip stream (the requested compression).
func TestExportInstanceProducesTarball(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("export")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	var buf bytes.Buffer
	require.NoError(t, b.ExportInstance(ctx, name, &buf))

	require.Positive(t, buf.Len(), "export should produce a non-empty backup")
	assert.Equal(t, []byte{0x1f, 0x8b}, buf.Bytes()[:2], "backup should be a gzip stream")
}

// TestExportImportRoundTrip exports a throwaway instance and imports the
// resulting tarball back under a new name, asserting the clone is listed.
func TestExportImportRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	src := uniqueName("exp-src")
	dst := uniqueName("exp-dst")
	t.Cleanup(func() {
		cleanupInstance(t, b, dst)
		cleanupInstance(t, b, src)
	})

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: src, Image: testImage}))

	var buf bytes.Buffer
	require.NoError(t, b.ExportInstance(ctx, src, &buf))
	require.NoError(t, b.ImportInstance(ctx, dst, &buf))

	list, err := b.ListInstances(ctx)
	require.NoError(t, err)
	assert.True(t, listed(list, dst), "imported instance %q should be listed", dst)
}

// TestConsoleLogReadsForRunningInstance starts a throwaway instance and reads
// its console log without error (content may be empty depending on the image).
func TestConsoleLogReadsForRunningInstance(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("console")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage, Start: true}))

	_, err := b.ConsoleLog(ctx, name)
	require.NoError(t, err)
}

// TestExecRunsCommandWithResize opens an interactive shell on a running
// instance, seeds a window size, runs a command, and asserts its output came
// back through the stdout bridge.
func TestExecRunsCommandWithResize(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("exec")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage, Start: true}))

	stdinR, stdinW := io.Pipe()
	var out bytes.Buffer
	resize := make(chan backend.WinSize, 1)
	resize <- backend.WinSize{Cols: 100, Rows: 40}

	done := make(chan error, 1)
	go func() {
		done <- b.Exec(ctx, name, backend.ExecRequest{
			Command: []string{"/bin/sh"},
			Stdin:   stdinR,
			Stdout:  &out,
			Resize:  resize,
			Width:   80,
			Height:  24,
		})
	}()

	if _, err := io.WriteString(stdinW, "echo lxcon-exec-ok\n"); err != nil {
		t.Fatalf("write command: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := io.WriteString(stdinW, "exit\n"); err != nil {
		t.Fatalf("write exit: %v", err)
	}
	require.NoError(t, stdinW.Close())

	require.NoError(t, <-done)
	assert.Contains(t, out.String(), "lxcon-exec-ok")
}

// TestRoundTripLifecycle covers the write paths: create+start, read back as
// Running, stop, delete (and confirm it leaves the list).
func TestRoundTripLifecycle(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("life")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	if err := b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage, Start: true}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if inst, err := b.GetInstance(ctx, name); err != nil || inst.Status != "Running" {
		t.Fatalf("want Running after create+start: inst=%+v err=%v", inst, err)
	}
	if err := b.StopInstance(ctx, name); err != nil {
		t.Fatalf("stop: %v", err)
	}
	inst, err := b.GetInstance(ctx, name)
	require.NoError(t, err)
	if inst.Status != "Stopped" {
		t.Fatalf("want Stopped after stop, got %q", inst.Status)
	}
	if err := b.DeleteInstance(ctx, name); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, err := b.ListInstances(ctx)
	require.NoError(t, err)
	if listed(list, name) {
		t.Fatal("instance still listed after delete")
	}
}

func TestRestartPauseResumeRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("lcyc")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	if err := b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage, Start: true}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if inst, err := b.GetInstance(ctx, name); err != nil || inst.Status != "Running" {
		t.Fatalf("want Running after create+start: inst=%+v err=%v", inst, err)
	}

	if err := b.RestartInstance(ctx, name); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if inst, err := b.GetInstance(ctx, name); err != nil || inst.Status != "Running" {
		t.Fatalf("want Running after restart: inst=%+v err=%v", inst, err)
	}

	if err := b.PauseInstance(ctx, name); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if inst, err := b.GetInstance(ctx, name); err != nil || inst.Status != "Frozen" {
		t.Fatalf("want Frozen after pause: inst=%+v err=%v", inst, err)
	}

	if err := b.ResumeInstance(ctx, name); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if inst, err := b.GetInstance(ctx, name); err != nil || inst.Status != "Running" {
		t.Fatalf("want Running after resume: inst=%+v err=%v", inst, err)
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
		cleanupInstance(t, b, clone)
		cleanupInstance(t, b, name)
	})

	if err := b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage, Start: true}); err != nil {
		t.Fatalf("create: %v", err)
	}
	inst, err := b.GetInstance(ctx, name)
	require.NoError(t, err)
	if inst.Status != "Running" {
		t.Fatalf("want Running, got %q", inst.Status)
	}

	if err := b.CreateSnapshot(ctx, name, "snap0"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	snaps, err := b.ListSnapshots(ctx, name)
	require.NoError(t, err)
	if len(snaps) != 1 || snaps[0].Name != "snap0" {
		t.Fatalf("snapshots: %+v", snaps)
	}

	// Stop before clone/restore to avoid running-state restrictions.
	if err := b.StopInstance(ctx, name); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if err := b.CloneInstance(ctx, name, clone); err != nil {
		t.Fatalf("clone: %v", err)
	}
	list, err := b.ListInstances(ctx)
	require.NoError(t, err)
	if !listed(list, name) || !listed(list, clone) {
		t.Fatalf("want both %q and %q listed: %+v", name, clone, list)
	}

	if err := b.RestoreSnapshot(ctx, name, "snap0"); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if err := b.DeleteSnapshot(ctx, name, "snap0"); err != nil {
		t.Fatalf("delete snapshot: %v", err)
	}
	snaps, err = b.ListSnapshots(ctx, name)
	require.NoError(t, err)
	if len(snaps) != 0 {
		t.Fatalf("want 0 snapshots after delete, got %d", len(snaps))
	}

	if err := b.DeleteInstance(ctx, clone); err != nil {
		t.Fatalf("delete clone: %v", err)
	}
	if err := b.DeleteInstance(ctx, name); err != nil {
		t.Fatalf("delete original: %v", err)
	}
	list, err = b.ListInstances(ctx)
	require.NoError(t, err)
	if listed(list, name) || listed(list, clone) {
		t.Fatal("instances still listed after delete")
	}
}
