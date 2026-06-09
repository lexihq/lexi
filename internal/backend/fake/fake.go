// Package fake is an in-memory backend.Backend used to drive fast, daemon-free
// unit tests of the HTTP and UI layers.
package fake

import (
	"context"
	"fmt"
	"io"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adam/lxcon/internal/backend"
)

// fakeBackupMagic prefixes the deterministic blob ExportInstance writes so
// ImportInstance can recognize a lxcon-produced backup and recover the image.
const fakeBackupMagic = "lxcon-fake-backup\n"

// Compile-time proof that Fake satisfies the Backend contract.
var _ backend.Backend = (*Fake)(nil)

type instance struct {
	backend.Instance

	snapshots []backend.Snapshot
	config    map[string]string
}

// Fake is a mutex-guarded, in-memory Backend with a deterministic clock.
type Fake struct {
	mu        sync.Mutex
	instances map[string]*instance
	profiles  map[string]backend.Profile
	clock     time.Time
}

// New returns an empty fake backend.
func New() *Fake {
	return &Fake{
		profiles: map[string]backend.Profile{
			"default": {
				Name: "default", Description: "Default Incus profile",
				Config: map[string]string{},
				Devices: map[string]map[string]string{
					"eth0": {"type": "nic", "network": "incusbr0"},
					"root": {"type": "disk", "path": "/", "pool": "default"},
				},
			},
			"gpu": {
				Name: "gpu", Description: "GPU passthrough",
				Config:  map[string]string{},
				Devices: map[string]map[string]string{"gpu0": {"type": "gpu"}},
			},
		},
		instances: make(map[string]*instance),
		clock:     time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
}

// now returns a deterministic, monotonically increasing timestamp.
// Callers must hold the mutex.
func (f *Fake) now() time.Time {
	f.clock = f.clock.Add(time.Second)
	return f.clock
}

func (f *Fake) Capabilities() backend.Capabilities {
	return backend.Capabilities{
		Tier:       backend.TierFake,
		ServerInfo: "fake backend",
		Snapshots:  true,
		Clone:      true,
		Backup:     true,
		Console:    true,
		Metrics:    true,
		Limits:     true,
		Pause:      true,
		Profiles:   true,
		Config:     true,
	}
}

// view materializes the public Instance with an up-to-date snapshot count.
// Callers must hold the mutex.
func (f *Fake) view(in *instance) backend.Instance {
	out := in.Instance
	out.Snapshots = len(in.snapshots)
	out.IPv4 = append([]string(nil), in.IPv4...)
	out.Profiles = append([]string(nil), in.Profiles...)
	return out
}

func (f *Fake) ListInstances(_ context.Context) ([]backend.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]backend.Instance, 0, len(f.instances))
	for _, in := range f.instances {
		out = append(out, f.view(in))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetInstance(_ context.Context, name string) (backend.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return backend.Instance{}, notFound(name)
	}
	return f.view(in), nil
}

func (f *Fake) CreateInstance(_ context.Context, opt backend.CreateOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[opt.Name]; ok {
		return conflict("instance %q already exists", opt.Name)
	}
	status := "Stopped"
	if opt.Start {
		status = "Running"
	}
	f.instances[opt.Name] = &instance{
		Instance: backend.Instance{
			Name:      opt.Name,
			Status:    status,
			Image:     opt.Image,
			CreatedAt: f.now(),
			Profiles:  []string{"default"},
		},
		config: map[string]string{},
	}
	return nil
}

func (f *Fake) ListProfiles(_ context.Context) ([]backend.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]backend.Profile, 0, len(f.profiles))
	for name := range f.profiles {
		out = append(out, f.profileView(name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetProfile(_ context.Context, name string) (backend.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.profiles[name]; !ok {
		return backend.Profile{}, notFoundf("profile %q", name)
	}
	return f.profileView(name), nil
}

func (f *Fake) SetInstanceProfiles(_ context.Context, name string, profiles []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	for _, p := range profiles {
		if _, ok := f.profiles[p]; !ok {
			return invalid("unknown profile %q", p)
		}
	}
	in.Profiles = append([]string(nil), profiles...)
	return nil
}

func (f *Fake) GetInstanceConfig(_ context.Context, name string) (backend.InstanceConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return backend.InstanceConfig{}, notFound(name)
	}
	cfg := maps.Clone(in.config)
	// Read-only devices = merge of the instance's assigned profiles' devices.
	devices := map[string]map[string]string{}
	for _, pn := range in.Profiles {
		p, ok := f.profiles[pn]
		if !ok {
			continue
		}
		for devName, dev := range p.Devices {
			devices[devName] = maps.Clone(dev)
		}
	}
	return backend.InstanceConfig{Config: cfg, Devices: devices}, nil
}

func (f *Fake) UpdateInstanceConfig(_ context.Context, name string, config map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	in.config = maps.Clone(config)
	return nil
}

// profileView materializes a profile with a fresh UsedBy from current instances.
// Callers must hold the mutex.
func (f *Fake) profileView(name string) backend.Profile {
	p := f.profiles[name]
	var usedBy []string
	for instName, in := range f.instances {
		for _, pn := range in.Profiles {
			if pn == name {
				usedBy = append(usedBy, instName)
			}
		}
	}
	sort.Strings(usedBy)
	p.UsedBy = usedBy
	return p
}

func (f *Fake) StartInstance(_ context.Context, name string) error {
	return f.setStatus(name, "Running")
}

func (f *Fake) StopInstance(_ context.Context, name string) error {
	return f.setStatus(name, "Stopped")
}

func (f *Fake) RestartInstance(_ context.Context, name string) error {
	return f.setStatus(name, "Running")
}

func (f *Fake) PauseInstance(_ context.Context, name string) error {
	return f.setStatus(name, "Frozen")
}

func (f *Fake) ResumeInstance(_ context.Context, name string) error {
	return f.setStatus(name, "Running")
}

func (f *Fake) setStatus(name, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	in.Status = status
	return nil
}

func (f *Fake) DeleteInstance(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[name]; !ok {
		return notFound(name)
	}
	delete(f.instances, name)
	return nil
}

func (f *Fake) ListSnapshots(_ context.Context, name string) ([]backend.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return nil, notFound(name)
	}
	return append([]backend.Snapshot(nil), in.snapshots...), nil
}

func (f *Fake) CreateSnapshot(_ context.Context, name, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	for _, s := range in.snapshots {
		if s.Name == snapshot {
			return conflict("snapshot %q already exists on %q", snapshot, name)
		}
	}
	in.snapshots = append(in.snapshots, backend.Snapshot{Name: snapshot, CreatedAt: f.now()})
	return nil
}

func (f *Fake) RestoreSnapshot(_ context.Context, name, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	for _, s := range in.snapshots {
		if s.Name == snapshot {
			return nil
		}
	}
	return notFoundf("snapshot %q not found on %q", snapshot, name)
}

func (f *Fake) DeleteSnapshot(_ context.Context, name, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	for i, s := range in.snapshots {
		if s.Name == snapshot {
			in.snapshots = append(in.snapshots[:i], in.snapshots[i+1:]...)
			return nil
		}
	}
	return notFoundf("snapshot %q not found on %q", snapshot, name)
}

func (f *Fake) CloneInstance(_ context.Context, src, dst string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	from, ok := f.instances[src]
	if !ok {
		return notFound(src)
	}
	if _, ok := f.instances[dst]; ok {
		return conflict("instance %q already exists", dst)
	}
	f.instances[dst] = &instance{
		Instance: backend.Instance{
			Name:      dst,
			Status:    "Stopped",
			Image:     from.Image,
			CreatedAt: f.now(),
		},
	}
	return nil
}

// Metrics returns deterministic canned counters for any existing instance, so
// handler and UI tests can assert the panel without a live daemon.
func (f *Fake) Metrics(_ context.Context, name string) (backend.Metrics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[name]; !ok {
		return backend.Metrics{}, notFound(name)
	}
	return backend.Metrics{
		CPUPercent:  12.5,
		MemoryUsage: 256 << 20,
		MemoryTotal: 1024 << 20,
		DiskUsage:   512 << 20,
		NetworkRx:   1 << 20,
		NetworkTx:   2 << 20,
		Processes:   7,
	}, nil
}

// ExportInstance writes a deterministic backup blob for an existing instance so
// handler tests can exercise the download path (and the C2 import round-trip)
// without a daemon.
func (f *Fake) ExportInstance(_ context.Context, name string, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	_, err := io.WriteString(w, fakeBackupMagic+in.Image)
	return err
}

// ImportInstance recreates an instance from a blob ExportInstance wrote. It
// validates the magic header (rejecting foreign data with ErrInvalid) and
// recovers the original image so the export→import round-trip is observable.
func (f *Fake) ImportInstance(_ context.Context, name string, r io.Reader) error {
	blob, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	image, ok := strings.CutPrefix(string(blob), fakeBackupMagic)
	if !ok {
		return fmt.Errorf("not a lxcon backup: %w", backend.ErrInvalid)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[name]; ok {
		return conflict("instance %q already exists", name)
	}
	f.instances[name] = &instance{
		Instance: backend.Instance{
			Name:      name,
			Status:    "Stopped",
			Image:     image,
			CreatedAt: f.now(),
		},
	}
	return nil
}

// Exec echoes stdin back to stdout for an existing instance, which is enough to
// assert the WebSocket bridge wiring without a live daemon. It ignores resize
// events. The instance check happens before any streaming.
func (f *Fake) Exec(_ context.Context, name string, req backend.ExecRequest) error {
	f.mu.Lock()
	_, ok := f.instances[name]
	f.mu.Unlock()
	if !ok {
		return notFound(name)
	}
	_, err := io.Copy(req.Stdout, req.Stdin)
	return err
}

// ConsoleLog returns canned console output for an existing instance so handler
// and UI tests can assert the logs panel without a live daemon.
func (f *Fake) ConsoleLog(_ context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[name]; !ok {
		return "", notFound(name)
	}
	return fmt.Sprintf("[fake console] %s booted\nlogin: ", name), nil
}

func (f *Fake) UpdateLimits(_ context.Context, name string, l backend.Limits) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[name]
	if !ok {
		return notFound(name)
	}
	in.LimitsCPU = l.CPU
	in.LimitsMemory = l.Memory
	return nil
}

// catalogImages stands in for the full simplestreams catalog the incus driver
// caches. It spans distributions, releases and architectures so handler-level
// filter tests have something to slice. Arches use incus naming.
var catalogImages = []backend.Image{
	{Alias: "debian/12", Fingerprint: "fake-debian-12-aarch64", Description: "Debian 12 (bookworm) arm64", Arch: "aarch64", Distribution: "debian", Release: "12", Variant: "default", Type: "container"},
	{Alias: "debian/12", Fingerprint: "fake-debian-12-x86-64", Description: "Debian 12 (bookworm) amd64", Arch: "x86_64", Distribution: "debian", Release: "12", Variant: "default", Type: "container"},
	{Alias: "ubuntu/24.04", Fingerprint: "fake-ubuntu-24-04-aarch64", Description: "Ubuntu 24.04 LTS arm64", Arch: "aarch64", Distribution: "ubuntu", Release: "24.04", Variant: "default", Type: "container"},
	{Alias: "ubuntu/24.04", Fingerprint: "fake-ubuntu-24-04-vm-x86-64", Description: "Ubuntu 24.04 LTS VM amd64", Arch: "x86_64", Distribution: "ubuntu", Release: "24.04", Variant: "default", Type: "virtual-machine"},
	{Alias: "alpine/edge", Fingerprint: "fake-alpine-edge-aarch64", Description: "Alpine Edge arm64", Arch: "aarch64", Distribution: "alpine", Release: "edge", Variant: "default", Type: "container"},
	{Alias: "fedora/40", Fingerprint: "fake-fedora-40-x86-64", Description: "Fedora 40 amd64", Arch: "x86_64", Distribution: "fedora", Release: "40", Variant: "default", Type: "container"},
}

func (f *Fake) ListImages(_ context.Context) ([]backend.Image, error) {
	return append([]backend.Image(nil), catalogImages...), nil
}

func notFound(name string) error {
	return fmt.Errorf("instance %q: %w", name, backend.ErrNotFound)
}

func notFoundf(format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), backend.ErrNotFound)
}

func conflict(format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), backend.ErrConflict)
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), backend.ErrInvalid)
}
