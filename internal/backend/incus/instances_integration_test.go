//go:build integration

package incus

import (
	"context"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/require"
)

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

// TestRebuildInstanceRoundTrip creates a stopped instance with a config key,
// rebuilds it from the catalog image, and asserts the instance survives with
// its config intact. A ghost instance is ErrNotFound.
func TestRebuildInstanceRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	if !b.Capabilities(ctx).InstanceRebuild {
		t.Skip("daemon lacks instances_rebuild")
	}
	name := uniqueName("rebuild")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{
		Name: name, Image: testImage,
		Config: map[string]string{"user.lexi": "keep"},
	}))

	require.NoError(t, b.RebuildInstance(ctx, name, testImage, ""))

	inst, err := b.GetInstance(ctx, name)
	require.NoError(t, err)
	require.Equal(t, backend.StatusStopped, inst.Status)
	cfg, err := b.GetInstanceConfig(ctx, name)
	require.NoError(t, err)
	require.Equal(t, "keep", cfg.Config["user.lexi"], "config must survive a rebuild")

	require.ErrorIs(t, b.RebuildInstance(ctx, uniqueName("ghost"), testImage, ""), backend.ErrNotFound)
}

// TestCreateWithOptionsRoundTrip creates an instance with an explicit profile
// list, root pool, network, and initial config, then asserts everything
// applied (profiles on the instance, root/eth0 local devices, config keys).
func TestCreateWithOptionsRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("opts")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	pools, err := b.ListStoragePools(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, pools, "need a storage pool")
	nets, err := b.ListNetworks(ctx)
	require.NoError(t, err)
	var network string
	for _, n := range nets {
		if n.Managed {
			network = n.Name
			break
		}
	}
	require.NotEmpty(t, network, "need a managed network")

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{
		Name: name, Image: testImage,
		Profiles: []string{"default"},
		Pool:     pools[0].Name,
		Network:  network,
		Config:   map[string]string{"limits.cpu": "1", "user.lexi": "yes"},
	}))

	inst, err := b.GetInstance(ctx, name)
	require.NoError(t, err)
	require.Equal(t, []string{"default"}, inst.Profiles)
	require.Equal(t, "1", inst.LimitsCPU)

	cfg, err := b.GetInstanceConfig(ctx, name)
	require.NoError(t, err)
	require.Equal(t, "yes", cfg.Config["user.lexi"])
	require.Equal(t, pools[0].Name, cfg.LocalDevices["root"]["pool"])
	require.Equal(t, network, cfg.LocalDevices["eth0"]["network"])
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

	if err := b.CreateSnapshot(ctx, name, "snap0", backend.SnapshotOptions{}); err != nil {
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
