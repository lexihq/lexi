package fake

import (
	"fmt"
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
	devices   map[string]map[string]string
	files     map[string][]byte // path → content; dirs implied by prefixes
}

type storagePool struct {
	backend.StoragePool

	volumes map[string]*storageVolume
}

type storageVolume struct {
	backend.StorageVolume

	snapshots []backend.StorageVolumeSnapshot
}

// Fake is a mutex-guarded, in-memory Backend with a deterministic clock.
type Fake struct {
	mu        sync.Mutex
	instances map[string]*instance
	profiles  map[string]backend.Profile
	networks  map[string]backend.Network
	pools     map[string]*storagePool
	images    map[string]*backend.LocalImage // keyed by fingerprint
	ops       []backend.Operation            // newest first, capped at maxOps
	opSeq     int
	clock     time.Time

	serverConfig        map[string]string
	serverConfigVersion int // bumped per update; the Get/Update version token
	certificates        []backend.Certificate
	warnings            []backend.Warning
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
		networks: map[string]backend.Network{
			"incusbr0": {
				Name: "incusbr0", Type: "bridge", Managed: true, Description: "Default bridge",
				Config: map[string]string{"ipv4.address": "10.0.3.1/24", "ipv4.nat": "true"},
			},
			"eth0": {Name: "eth0", Type: "physical", Managed: false},
		},
		pools: map[string]*storagePool{
			"default": {StoragePool: backend.StoragePool{Name: "default", Driver: "dir", Description: "Default pool", Config: map[string]string{}}, volumes: map[string]*storageVolume{}},
			"zfs0":    {StoragePool: backend.StoragePool{Name: "zfs0", Driver: "zfs", Config: map[string]string{}}, volumes: map[string]*storageVolume{}},
		},
		images: map[string]*backend.LocalImage{
			"fake-debian-12-aarch64": {
				Fingerprint: "fake-debian-12-aarch64",
				Aliases:     []string{"debian/12"},
				Description: "Debian 12 (bookworm) arm64",
				Arch:        "aarch64",
				Type:        "container",
				CreatedAt:   time.Date(2025, time.December, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		serverConfig:        map[string]string{"core.https_address": ":8443"},
		serverConfigVersion: 1,
		certificates: []backend.Certificate{
			{Name: "admin-laptop", Type: "client", Fingerprint: "fake-cert-fingerprint-1234", Restricted: false},
		},
		warnings: []backend.Warning{
			{
				UUID: "fake-warning-1", Type: "Couldn't find the CGroup network priority controller",
				Severity: "low", Status: "new", Count: 3,
				LastMessage: "Couldn't find the CGroup network priority controller",
				LastSeenAt:  time.Date(2025, time.December, 31, 12, 0, 0, 0, time.UTC),
			},
			{
				UUID: "fake-warning-2", Type: "Instance type not operational",
				Severity: "moderate", Status: "acknowledged", Count: 1,
				LastMessage: "KVM support is missing (no /dev/kvm)",
				LastSeenAt:  time.Date(2025, time.December, 30, 9, 0, 0, 0, time.UTC),
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
		Devices:    true,
		Networks:   true,
		Storage:    true,
		Move:       true,

		ImageManagement: true,
		Operations:      true,
		Files:           true,
		ServerAdmin:     true,
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
