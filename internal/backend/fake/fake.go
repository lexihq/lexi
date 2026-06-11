package fake

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

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
	files     map[string]*fakeFile // clean absolute path → node; dirs are explicit entries
	// configVersion bumps on every config/device mutation; the
	// GetInstanceConfig/UpdateDevice version token.
	configVersion int
}

type storagePool struct {
	backend.StoragePool

	volumes map[string]*storageVolume
	// version is the counter behind the Get/Update concurrency token, bumped
	// on every pool config update.
	version int
}

type storageVolume struct {
	backend.StorageVolume

	snapshots []backend.StorageVolumeSnapshot
	// version is the counter behind the Get/Update concurrency token, bumped
	// on every volume config update.
	version int
}

// Fake is a mutex-guarded, in-memory Backend with a deterministic clock.
type Fake struct {
	mu        sync.Mutex
	instances map[string]*instance
	profiles  map[string]backend.Profile
	// profileVersions are per-profile counters bumped on update; the
	// Get/Update version token (missing key reads as 0).
	profileVersions map[string]int
	networks        map[string]backend.Network
	// networkVersions are per-network counters bumped on update; the
	// Get/Update version token (missing key reads as 0).
	networkVersions map[string]int
	acls            map[string]backend.NetworkACL
	// aclVersions are per-ACL counters bumped on update; the Get/Update
	// version token (missing key reads as 0).
	aclVersions map[string]int
	pools       map[string]*storagePool
	images      map[string]*backend.LocalImage // keyed by fingerprint
	ops         []backend.Operation            // newest first, capped at maxOps
	opSeq       int
	clock       time.Time

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
		profileVersions: map[string]int{},
		networkVersions: map[string]int{},
		acls:            map[string]backend.NetworkACL{},
		aclVersions:     map[string]int{},
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
		FileDelete:      true,
		FileMkdir:       true,
		ServerAdmin:     true,
		NetworkACLs:     true,
		VolumeBackups:   true,
	}
}

// view materializes an instance with derived fields. Limits mirror the real
// driver, which reads them from the daemon's expanded config: an instance-local
// limit wins, else the last assigned profile that sets the key (later profiles
// override earlier ones). Callers must hold the mutex.
func (f *Fake) view(in *instance) backend.Instance {
	out := in.Instance
	out.Snapshots = len(in.snapshots)
	out.IPv4 = append([]string(nil), in.IPv4...)
	out.Profiles = append([]string(nil), in.Profiles...)
	if out.LimitsCPU == "" {
		out.LimitsCPU = f.profileConfigValue(in.Profiles, "limits.cpu")
	}
	if out.LimitsMemory == "" {
		out.LimitsMemory = f.profileConfigValue(in.Profiles, "limits.memory")
	}
	return out
}

// profileConfigValue returns key's value from the last profile in override
// order that sets it, or "". Callers must hold the mutex.
func (f *Fake) profileConfigValue(profiles []string, key string) string {
	for _, name := range slices.Backward(profiles) {
		if v, ok := f.profiles[name].Config[key]; ok {
			return v
		}
	}
	return ""
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

// splitCommaList splits a comma-separated config value (e.g. security.acls)
// into trimmed, non-empty entries.
func splitCommaList(v string) []string {
	var out []string
	for s := range strings.SplitSeq(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// validAPIName reports whether name passes the daemon's validate.IsAPIName
// rules (≤64 chars, no whitespace, none of the reserved URL characters), so
// fake-backed tests reject the same names production does.
func validAPIName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return !strings.ContainsAny(name, `$?&+"'`+"`*/")
}
