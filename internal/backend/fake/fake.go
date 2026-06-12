package fake

import (
	"context"
	"fmt"
	"regexp"
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
	backups   map[string]*storedBackup // server-side named backups, by name
	config    map[string]string
	devices   map[string]map[string]string
	files     map[string]*fakeFile // clean absolute path → node; dirs are explicit entries
	// configVersion bumps on every config/device mutation; the
	// GetInstanceConfig/UpdateDevice version token.
	configVersion int
}

type storagePool struct {
	backend.StoragePool

	volumes map[string]map[string]*storageVolume // project → volume name → volume
	// version is the counter behind the Get/Update concurrency token, bumped
	// on every pool config update.
	version int
}

// vols returns the pool's volume namespace for a project, creating it lazily.
func (p *storagePool) vols(project string) map[string]*storageVolume {
	m, ok := p.volumes[project]
	if !ok {
		m = map[string]*storageVolume{}
		p.volumes[project] = m
	}
	return m
}

type storageVolume struct {
	backend.StorageVolume

	snapshots []backend.StorageVolumeSnapshot
	// version is the counter behind the Get/Update concurrency token, bumped
	// on every volume config update.
	version int
}

// space is the project-scoped slice of the fake's state. The *Versions maps
// are per-object counters bumped on update — the Get/Update version tokens
// (missing keys read as 0). networks/acls are populated only for projects
// with features.networks=true; everyone else shares the default space's (see
// networkSpace).
type space struct {
	instances       map[string]*instance
	profiles        map[string]backend.Profile
	profileVersions map[string]int
	networks        map[string]backend.Network
	networkVersions map[string]int
	acls            map[string]backend.NetworkACL
	aclVersions     map[string]int
	// forwards are port forwards keyed by network, then listen address.
	// Deliberately unversioned: the daemon enforces no etag on forwards.
	forwards map[string]map[string]backend.NetworkForward
	images   map[string]*backend.LocalImage // keyed by fingerprint
	ops      []backend.Operation            // newest first, capped at maxOps
	opSeq    int
	ipSeq    int // DHCP-ish counter for addresses handed to started instances
}

// newSpace returns an empty project space.
func newSpace() *space {
	return &space{
		instances:       map[string]*instance{},
		profiles:        map[string]backend.Profile{},
		profileVersions: map[string]int{},
		networks:        map[string]backend.Network{},
		networkVersions: map[string]int{},
		acls:            map[string]backend.NetworkACL{},
		aclVersions:     map[string]int{},
		forwards:        map[string]map[string]backend.NetworkForward{},
		images:          map[string]*backend.LocalImage{},
	}
}

// Fake is a mutex-guarded, in-memory Backend with a deterministic clock.
// remoteState is one fake daemon: everything a single Incus server owns.
// Project-scoped state lives in its spaces; pools, certificates, and server
// config are daemon-global, like Incus.
type remoteState struct {
	spaces   map[string]*space // keyed by project; "default" always exists
	projects map[string]backend.Project
	// projectVersions are per-project counters bumped on update; the
	// Get/Update version token (missing key reads as 0).
	projectVersions map[string]int
	pools           map[string]*storagePool

	serverConfig        map[string]string
	serverConfigVersion int // bumped per update; the Get/Update version token
	certificates        []backend.Certificate
	warnings            []backend.Warning
}

// Fake models a set of independent daemons (remotes), each with its own
// remoteState; "local" is the default remote and always exists.
type Fake struct {
	mu      sync.Mutex
	remotes map[string]*remoteState
	clock   time.Time

	// opWatchers receive a coalesced tick whenever an operation is recorded
	// or changed; keyed by a registration sequence so cancellation can
	// unregister exactly its own channel.
	opWatchers  map[int]chan struct{}
	opWatcherID int
}

// projectOf normalizes the request's project; unset means default.
func projectOf(ctx context.Context) string {
	if name := backend.ProjectFromContext(ctx); name != "" {
		return name
	}
	return "default"
}

// remoteOf normalizes the request's remote; unset means local.
func remoteOf(ctx context.Context) string {
	if name := backend.RemoteFromContext(ctx); name != "" {
		return name
	}
	return "local"
}

// remote returns the request's daemon state, creating it lazily (a ghost
// remote's state is empty and invisible — the HTTP layer validates remote
// existence against ListRemotes). Callers must hold the mutex.
func (f *Fake) remote(ctx context.Context) *remoteState {
	return f.remoteFor(remoteOf(ctx))
}

// remoteFor returns the named remote's state, creating it lazily. Callers
// must hold the mutex.
func (f *Fake) remoteFor(name string) *remoteState {
	rs, ok := f.remotes[name]
	if !ok {
		rs = &remoteState{
			spaces:          map[string]*space{},
			projects:        map[string]backend.Project{},
			projectVersions: map[string]int{},
			pools:           map[string]*storagePool{},
			serverConfig:    map[string]string{},
		}
		f.remotes[name] = rs
	}
	return rs
}

// space returns the request's project space, creating it lazily (a ghost
// project's space is empty and invisible — the HTTP layer validates project
// existence). Callers must hold the mutex.
func (f *Fake) space(ctx context.Context) *space {
	return f.remote(ctx).spaceFor(projectOf(ctx))
}

// spaceFor returns the named project's space within this remote, creating it
// lazily. Callers must hold the Fake mutex.
func (rs *remoteState) spaceFor(project string) *space {
	sp, ok := rs.spaces[project]
	if !ok {
		sp = newSpace()
		rs.spaces[project] = sp
	}
	return sp
}

// featureSpace returns the space owning a feature-routed resource kind: the
// project's own when it enables the feature, else the default project's
// (Incus shares such resources from default otherwise). Callers must hold
// the mutex.
func (f *Fake) featureSpace(ctx context.Context, feature string) *space {
	return f.remote(ctx).spaceFor(f.featureProject(ctx, feature))
}

// featureProject names the project owning a feature-routed resource kind for
// this request. Callers must hold the mutex.
func (f *Fake) featureProject(ctx context.Context, feature string) string {
	return f.remote(ctx).featureProjectName(projectOf(ctx), feature)
}

// featureProjectName is featureProject for an explicit project name; usage
// scans use it to decide which projects share a resource owner's namespace.
// Callers must hold the Fake mutex.
func (rs *remoteState) featureProjectName(project, feature string) string {
	if project == "default" || rs.projects[project].Config[feature] == "true" {
		return project
	}
	return "default"
}

// networkSpace returns the space owning the request project's networks and
// ACLs. Callers must hold the mutex.
func (f *Fake) networkSpace(ctx context.Context) *space {
	return f.featureSpace(ctx, "features.networks")
}

// New returns an empty fake backend.
func New() *Fake {
	defaultSpace := newSpace()
	defaultSpace.profiles = map[string]backend.Profile{
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
	}
	defaultSpace.networks = map[string]backend.Network{
		"incusbr0": {
			Name: "incusbr0", Type: "bridge", Managed: true, Description: "Default bridge",
			Config: map[string]string{"ipv4.address": "10.0.3.1/24", "ipv4.nat": "true"},
		},
		"eth0": {Name: "eth0", Type: "physical", Managed: false},
	}
	defaultSpace.images = map[string]*backend.LocalImage{
		"fake-debian-12-aarch64": {
			Fingerprint: "fake-debian-12-aarch64",
			Aliases:     []string{"debian/12"},
			Description: "Debian 12 (bookworm) arm64",
			Arch:        "aarch64",
			Type:        "container",
			CreatedAt:   time.Date(2025, time.December, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	local := &remoteState{
		spaces: map[string]*space{"default": defaultSpace},
		projects: map[string]backend.Project{
			"default": {Name: "default", Description: "Default Incus project", Config: map[string]string{
				"features.images": "true", "features.networks": "true",
				"features.networks.zones": "true", "features.profiles": "true",
				"features.storage.buckets": "true", "features.storage.volumes": "true",
			}},
		},
		projectVersions: map[string]int{},
		pools: map[string]*storagePool{
			"default": {StoragePool: backend.StoragePool{Name: "default", Driver: "dir", Description: "Default pool", Config: map[string]string{}}, volumes: map[string]map[string]*storageVolume{}},
			"zfs0":    {StoragePool: backend.StoragePool{Name: "zfs0", Driver: "zfs", Config: map[string]string{}}, volumes: map[string]map[string]*storageVolume{}},
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
	}
	return &Fake{
		remotes: map[string]*remoteState{
			"local":     local,
			"secondary": newSecondaryRemote(),
		},
		clock: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
}

// newSecondaryRemote seeds the fake's second daemon: the bare defaults a
// fresh Incus install has (default project/profile/network/pool plus the
// catalog image instances launch from), and none of local's instances or
// server state — switching remotes must visibly change everything.
func newSecondaryRemote() *remoteState {
	sp := newSpace()
	sp.profiles = map[string]backend.Profile{
		"default": {
			Name: "default", Description: "Default Incus profile",
			Config: map[string]string{},
			Devices: map[string]map[string]string{
				"eth0": {"type": "nic", "network": "incusbr0"},
				"root": {"type": "disk", "path": "/", "pool": "default"},
			},
		},
	}
	sp.networks = map[string]backend.Network{
		"incusbr0": {
			Name: "incusbr0", Type: "bridge", Managed: true, Description: "Default bridge",
			Config: map[string]string{"ipv4.address": "10.0.4.1/24", "ipv4.nat": "true"},
		},
	}
	sp.images = map[string]*backend.LocalImage{
		"fake-debian-12-aarch64": {
			Fingerprint: "fake-debian-12-aarch64",
			Aliases:     []string{"debian/12"},
			Description: "Debian 12 (bookworm) arm64",
			Arch:        "aarch64",
			Type:        "container",
			CreatedAt:   time.Date(2025, time.December, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	return &remoteState{
		spaces: map[string]*space{"default": sp},
		projects: map[string]backend.Project{
			"default": {Name: "default", Description: "Default Incus project", Config: map[string]string{
				"features.images": "true", "features.networks": "true",
				"features.profiles": "true", "features.storage.volumes": "true",
			}},
		},
		projectVersions: map[string]int{},
		pools: map[string]*storagePool{
			"default": {StoragePool: backend.StoragePool{Name: "default", Driver: "dir", Description: "Default pool", Config: map[string]string{}}, volumes: map[string]map[string]*storageVolume{}},
		},
		serverConfig:        map[string]string{"core.https_address": ":8444"},
		serverConfigVersion: 1,
	}
}

// now returns a deterministic, monotonically increasing timestamp.
// Callers must hold the mutex.
func (f *Fake) now() time.Time {
	f.clock = f.clock.Add(time.Second)
	return f.clock
}

func (f *Fake) Capabilities(_ context.Context) backend.Capabilities {
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
		Projects:        true,
		Events:          true,
		Remotes:         true,
		Migrate:         true,
		NetworkForwards: true,
		ImageRefresh:    true,
		StoredBackups:   true,
		CertificateEdit: true,
		InstanceRebuild: true,
		ISOVolumes:      true,
	}
}

// view materializes an instance with derived fields. Limits mirror the real
// driver, which reads them from the daemon's expanded config: an instance-local
// limit wins, else the last assigned profile that sets the key (later profiles
// override earlier ones). Callers must hold the mutex.
func (f *Fake) view(sp *space, in *instance) backend.Instance {
	out := in.Instance
	out.Snapshots = len(in.snapshots)
	out.IPv4 = append([]string(nil), in.IPv4...)
	out.Profiles = append([]string(nil), in.Profiles...)
	if out.LimitsCPU == "" {
		out.LimitsCPU = profileConfigValue(sp, in.Profiles, "limits.cpu")
	}
	if out.LimitsMemory == "" {
		out.LimitsMemory = profileConfigValue(sp, in.Profiles, "limits.memory")
	}
	return out
}

// profileConfigValue returns key's value from the last profile in override
// order that sets it, or "". Callers must hold the mutex.
func profileConfigValue(sp *space, profiles []string, key string) string {
	for _, name := range slices.Backward(profiles) {
		if v, ok := sp.profiles[name].Config[key]; ok {
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

// apiNameEnds is the daemon's IsAPIName tail rule: names must start and end
// with an alphanumeric character (which implies a minimum of two). It applies
// where the daemon runs the full IsAPIName (projects, volume creation), not
// to snapshot names.
var apiNameEnds = regexp.MustCompile(`^[a-zA-Z0-9]+.*[a-zA-Z0-9]+$`)
