package fake

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/adam/lxcon/internal/backend"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecEchoesStdin(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	var out bytes.Buffer
	require.NoError(t, b.Exec(ctx(), "demo", backend.ExecRequest{
		Stdin:  strings.NewReader("hello\n"),
		Stdout: &out,
	}))
	assert.Equal(t, "hello\n", out.String(), "fake exec should echo stdin to stdout")

	// Missing instance → ErrNotFound, before any streaming.
	err := b.Exec(ctx(), "ghost", backend.ExecRequest{Stdin: strings.NewReader(""), Stdout: &out})
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestConsoleLog(t *testing.T) {
	b := New()
	assert.True(t, b.Capabilities().Console, "fake should advertise console")
	mustCreate(t, b, "demo")

	log, err := b.ConsoleLog(ctx(), "demo")
	require.NoError(t, err)
	assert.NotEmpty(t, log, "console log should return canned text")

	_, err = b.ConsoleLog(ctx(), "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestImportInstanceRoundTrip(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo") // image debian/12

	var buf bytes.Buffer
	require.NoError(t, b.ExportInstance(ctx(), "demo", &buf))

	// A round-trip recreates the instance under a new name, preserving the image.
	require.NoError(t, b.ImportInstance(ctx(), "restored", &buf))
	inst, err := b.GetInstance(ctx(), "restored")
	require.NoError(t, err)
	assert.Equal(t, "debian/12", inst.Image)

	// Importing onto an existing name → ErrConflict.
	buf.Reset()
	require.NoError(t, b.ExportInstance(ctx(), "demo", &buf))
	require.ErrorIs(t, b.ImportInstance(ctx(), "demo", &buf), backend.ErrConflict)

	// A blob that isn't a lxcon backup → ErrInvalid.
	require.ErrorIs(t, b.ImportInstance(ctx(), "x", strings.NewReader("garbage")), backend.ErrInvalid)
}

func TestExportInstance(t *testing.T) {
	b := New()
	assert.True(t, b.Capabilities().Backup, "fake should advertise backup")
	mustCreate(t, b, "demo")

	var buf bytes.Buffer
	require.NoError(t, b.ExportInstance(ctx(), "demo", &buf))
	assert.NotEmpty(t, buf.Bytes(), "export should write a backup blob")

	// Missing instance → ErrNotFound.
	require.ErrorIs(t, b.ExportInstance(ctx(), "ghost", &buf), backend.ErrNotFound)
}

func TestMetrics(t *testing.T) {
	b := New()
	assert.True(t, b.Capabilities().Metrics, "fake should advertise metrics")
	mustCreate(t, b, "demo")

	m, err := b.Metrics(ctx(), "demo")
	require.NoError(t, err)
	assert.Positive(t, m.MemoryUsage)
	assert.Positive(t, m.MemoryTotal)

	_, err = b.Metrics(ctx(), "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestUpdateLimits(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	require.NoError(t, b.UpdateLimits(ctx(), "demo", backend.Limits{CPU: "2", Memory: "2GiB"}))
	inst, err := b.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "2", inst.LimitsCPU)
	assert.Equal(t, "2GiB", inst.LimitsMemory)

	// Empty limits clear the values.
	require.NoError(t, b.UpdateLimits(ctx(), "demo", backend.Limits{}))
	inst, err = b.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Empty(t, inst.LimitsCPU)
	assert.Empty(t, inst.LimitsMemory)

	// Missing instance → ErrNotFound.
	require.ErrorIs(t, b.UpdateLimits(ctx(), "ghost", backend.Limits{}), backend.ErrNotFound)
}

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
	require.NoError(t, b.CreateSnapshot(ctx(), "demo", "s1"))
	require.ErrorIs(t, b.CreateSnapshot(ctx(), "demo", "s1"), backend.ErrConflict)
}

func ctx() context.Context { return context.Background() }

func TestCapabilitiesAdvertisesSnapshotAndClone(t *testing.T) {
	caps := New().Capabilities()
	if !caps.Snapshots || !caps.Clone {
		t.Fatalf("fake should support snapshots and clone, got %+v", caps)
	}
	if !caps.Pause {
		t.Fatalf("fake should support pause/resume, got %+v", caps)
	}
	if caps.ServerInfo == "" {
		t.Fatal("ServerInfo should be set")
	}
}

func TestRestartPauseResumeLifecycle(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo", Start: true}))

	require.NoError(t, f.RestartInstance(ctx(), "demo"))
	got, err := f.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "Running", got.Status)

	require.NoError(t, f.PauseInstance(ctx(), "demo"))
	got, err = f.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "Frozen", got.Status)

	require.NoError(t, f.ResumeInstance(ctx(), "demo"))
	got, err = f.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "Running", got.Status)

	require.ErrorIs(t, f.RestartInstance(ctx(), "ghost"), backend.ErrNotFound)
	require.ErrorIs(t, f.PauseInstance(ctx(), "ghost"), backend.ErrNotFound)
	require.ErrorIs(t, f.ResumeInstance(ctx(), "ghost"), backend.ErrNotFound)
}

func TestProfilesListAndGet(t *testing.T) {
	f := New()
	profiles, err := f.ListProfiles(ctx())
	require.NoError(t, err)
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, p.Name)
	}
	assert.Contains(t, names, "default")
	assert.Contains(t, names, "gpu")

	gpu, err := f.GetProfile(ctx(), "gpu")
	require.NoError(t, err)
	assert.NotEmpty(t, gpu.Devices, "gpu profile should carry a sample device")

	_, err = f.GetProfile(ctx(), "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestNewInstanceDefaultsToDefaultProfile(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo"}))
	inst, err := f.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, inst.Profiles)
}

func TestSetInstanceProfiles(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo"}))

	require.NoError(t, f.SetInstanceProfiles(ctx(), "demo", []string{"default", "gpu"}))
	inst, err := f.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"default", "gpu"}, inst.Profiles)

	gpu, err := f.GetProfile(ctx(), "gpu")
	require.NoError(t, err)
	assert.Contains(t, gpu.UsedBy, "demo")

	require.ErrorIs(t, f.SetInstanceProfiles(ctx(), "demo", []string{"nope"}), backend.ErrInvalid)
	require.ErrorIs(t, f.SetInstanceProfiles(ctx(), "ghost", []string{"default"}), backend.ErrNotFound)
}

func TestCreateListGet(t *testing.T) {
	b := New()

	if list, err := b.ListInstances(ctx()); err != nil || len(list) != 0 {
		t.Fatalf("fresh backend should be empty: list=%v err=%v", list, err)
	}

	if err := b.CreateInstance(ctx(), backend.CreateOptions{Name: "demo", Image: "debian/12"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := b.ListInstances(ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 instance, got %d", len(list))
	}
	got := list[0]
	if got.Name != "demo" || got.Status != "Stopped" || got.Image != "debian/12" {
		t.Fatalf("unexpected instance: %+v", got)
	}

	// Duplicate name is rejected.
	if err := b.CreateInstance(ctx(), backend.CreateOptions{Name: "demo", Image: "x"}); err == nil {
		t.Fatal("expected error creating duplicate name")
	}

	// Get on missing instance errors.
	if _, err := b.GetInstance(ctx(), "ghost"); err == nil {
		t.Fatal("expected error getting missing instance")
	}
}

func TestCreateWithStartIsRunning(t *testing.T) {
	b := New()
	if err := b.CreateInstance(ctx(), backend.CreateOptions{Name: "web", Image: "alpine/edge", Start: true}); err != nil {
		t.Fatalf("create: %v", err)
	}
	inst, err := b.GetInstance(ctx(), "web")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if inst.Status != "Running" {
		t.Fatalf("want Running, got %q", inst.Status)
	}
}

func TestStartStop(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	if err := b.StartInstance(ctx(), "demo"); err != nil {
		t.Fatalf("start: %v", err)
	}
	inst, err := b.GetInstance(ctx(), "demo")
	if err != nil {
		t.Fatalf("get after start: %v", err)
	}
	if inst.Status != "Running" {
		t.Fatalf("want Running after start, got %q", inst.Status)
	}

	if err := b.StopInstance(ctx(), "demo"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	inst, err = b.GetInstance(ctx(), "demo")
	if err != nil {
		t.Fatalf("get after stop: %v", err)
	}
	if inst.Status != "Stopped" {
		t.Fatalf("want Stopped after stop, got %q", inst.Status)
	}

	if err := b.StartInstance(ctx(), "missing"); err == nil {
		t.Fatal("expected error starting missing instance")
	}
}

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

func TestClone(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	if err := b.CloneInstance(ctx(), "demo", "demo-copy"); err != nil {
		t.Fatalf("clone: %v", err)
	}
	list, err := b.ListInstances(ctx())
	if err != nil {
		t.Fatalf("list after clone: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 instances after clone, got %d", len(list))
	}
	cp, err := b.GetInstance(ctx(), "demo-copy")
	if err != nil {
		t.Fatalf("get clone: %v", err)
	}
	if cp.Status != "Stopped" {
		t.Fatalf("clone should be Stopped, got %q", cp.Status)
	}

	// Cloning onto an existing name errors; cloning a missing source errors.
	if err := b.CloneInstance(ctx(), "demo", "demo-copy"); err == nil {
		t.Fatal("expected error cloning onto existing name")
	}
	if err := b.CloneInstance(ctx(), "ghost", "x"); err == nil {
		t.Fatal("expected error cloning missing source")
	}
}

func TestDelete(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	if err := b.DeleteInstance(ctx(), "demo"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, err := b.ListInstances(ctx())
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("want 0 instances after delete, got %d", len(list))
	}

	if err := b.DeleteInstance(ctx(), "demo"); err == nil {
		t.Fatal("expected error deleting missing instance")
	}
}

func TestListImagesCurated(t *testing.T) {
	imgs, err := New().ListImages(ctx())
	if err != nil {
		t.Fatalf("list images: %v", err)
	}
	if len(imgs) == 0 {
		t.Fatal("expected a curated image set, got none")
	}
	want := map[string]bool{"debian/12": false, "ubuntu/24.04": false, "alpine/edge": false}
	for _, img := range imgs {
		if _, ok := want[img.Alias]; ok {
			want[img.Alias] = true
		}
	}
	for alias, found := range want {
		if !found {
			t.Errorf("curated image %q missing from %+v", alias, imgs)
		}
	}
}

func mustCreate(t *testing.T, b *Fake, name string) {
	t.Helper()
	if err := b.CreateInstance(ctx(), backend.CreateOptions{Name: name, Image: "debian/12"}); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
}
