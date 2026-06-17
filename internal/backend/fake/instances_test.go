package fake

import (
	"errors"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestRebuildInstanceSwapsImage(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	require.NoError(t, f.RebuildInstance(ctx(), "demo", "alpine/3.20", ""))

	got, err := f.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "alpine/3.20", got.Image)
	assert.Equal(t, "Stopped", got.Status)
}

func TestRebuildInstanceRunningIsInvalid(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo", Image: "debian/12", Start: true}))

	err := f.RebuildInstance(ctx(), "demo", "alpine/3.20", "")
	require.ErrorIs(t, err, backend.ErrInvalid)

	got, err := f.GetInstance(ctx(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "debian/12", got.Image, "failed rebuild must not change the image")
}

func TestRebuildInstanceGhostIs404(t *testing.T) {
	err := New().RebuildInstance(ctx(), "ghost", "alpine/3.20", "")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestRebuildInstanceNoImageIsInvalid(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))
	err := f.RebuildInstance(ctx(), "demo", "", "")
	require.ErrorIs(t, err, backend.ErrInvalid)
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

func TestCreateWithProfilesPoolNetworkConfig(t *testing.T) {
	b := New()
	if err := b.CreateInstance(ctx(), backend.CreateOptions{
		Name: "web", Image: "debian/12",
		Profiles: []string{"default", "gpu"},
		Pool:     "default",
		Network:  "incusbr0",
		Config:   map[string]string{"limits.cpu": "2", "user.user-data": "#cloud-config\n"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	inst, err := b.GetInstance(ctx(), "web")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(inst.Profiles) != 2 || inst.Profiles[0] != "default" || inst.Profiles[1] != "gpu" {
		t.Fatalf("profiles not applied: %v", inst.Profiles)
	}

	cfg, err := b.GetInstanceConfig(ctx(), "web")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg.Config["user.user-data"] != "#cloud-config\n" {
		t.Fatalf("config not applied: %v", cfg.Config)
	}
	root := cfg.LocalDevices["root"]
	if root["type"] != "disk" || root["pool"] != "default" || root["path"] != "/" {
		t.Fatalf("root device not injected: %v", root)
	}
	eth0 := cfg.LocalDevices["eth0"]
	if eth0["type"] != "nic" || eth0["network"] != "incusbr0" {
		t.Fatalf("eth0 device not injected: %v", eth0)
	}

	// limits land in the Instance view like UpdateLimits would set them.
	if inst.LimitsCPU != "2" {
		t.Fatalf("limits.cpu not reflected: %q", inst.LimitsCPU)
	}
}

func TestCreateRejectsGhostReferences(t *testing.T) {
	b := New()
	if err := b.CreateInstance(ctx(), backend.CreateOptions{Name: "a", Image: "x", Profiles: []string{"ghost"}}); !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("ghost profile: want ErrInvalid, got %v", err)
	}
	if err := b.CreateInstance(ctx(), backend.CreateOptions{Name: "b", Image: "x", Pool: "ghost"}); !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("ghost pool: want ErrNotFound, got %v", err)
	}
	if err := b.CreateInstance(ctx(), backend.CreateOptions{Name: "c", Image: "x", Network: "ghost"}); !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("ghost network: want ErrNotFound, got %v", err)
	}
	// The seeded "eth0" network is unmanaged: the nic device shape only works
	// for managed networks, so the create is refused.
	if err := b.CreateInstance(ctx(), backend.CreateOptions{Name: "d", Image: "x", Network: "eth0"}); !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("unmanaged network: want ErrInvalid, got %v", err)
	}
	// Failed creates must not leave partial instances behind.
	if _, err := b.GetInstance(ctx(), "a"); !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("partial instance a left behind: %v", err)
	}
}

func TestInstanceLimitsInheritProfileConfig(t *testing.T) {
	b := New()
	require.NoError(t, b.CreateProfile(ctx(), "big", ""))
	require.NoError(t, b.UpdateProfile(ctx(), "big", "", map[string]string{"limits.cpu": "8", "limits.memory": "8GiB"}, ""))
	require.NoError(t, b.CreateInstance(ctx(), backend.CreateOptions{Name: "web", Image: "x", Profiles: []string{"default", "big"}}))

	inst, err := b.GetInstance(ctx(), "web")
	require.NoError(t, err)
	assert.Equal(t, "8", inst.LimitsCPU, "profile-supplied limits surface on the view")
	assert.Equal(t, "8GiB", inst.LimitsMemory)

	// An instance-local limit overrides the profile's.
	require.NoError(t, b.UpdateLimits(ctx(), "web", backend.Limits{CPU: "2", Memory: "8GiB"}))
	inst, err = b.GetInstance(ctx(), "web")
	require.NoError(t, err)
	assert.Equal(t, "2", inst.LimitsCPU)
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
