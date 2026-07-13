// Package backend defines the driver-agnostic contract lexi drives the UI
// through. Domain types are intentionally decoupled from incus/shared/api so a
// future liblxc driver can implement the same interface without a rewrite.
package backend

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// Sentinel errors drivers wrap (with %w) so the HTTP layer can map them to
// status codes via errors.Is, independent of any driver's wording.
var (
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("already exists")
	ErrInvalid     = errors.New("invalid")
	ErrUnsupported = errors.New("unsupported")
)

// Tier identifies which driver is serving requests.
type Tier string

const (
	TierIncus  Tier = "incus"
	TierLiblxc Tier = "liblxc"
	TierFake   Tier = "fake" // in-memory test double
)

// Capabilities lets the UI gracefully hide what a tier can't do.
type Capabilities struct {
	Tier       Tier
	ServerInfo string // e.g. "Incus 6.0.4"
	Snapshots  bool
	Clone      bool
	Backup     bool // portable export/import tarballs
	Console    bool // interactive terminal (exec over WebSocket)
	Metrics    bool // live resource metrics
	Limits     bool // CPU and memory limits
	Pause      bool // freeze/unfreeze (pause/resume)
	Profiles   bool // list/view profiles + attach to instances
	Config     bool // edit arbitrary config keys + view devices
	Devices    bool // add/remove instance-local devices
	Networks   bool // list/inspect/create/delete networks
	Storage    bool // list pools + custom volume/snapshot management
	Move       bool // rename + relocate to another storage pool
	// ImageManagement is local image store management: publish, copy from the
	// images remote, delete, and alias ops.
	ImageManagement bool
	// Operations is the daemon task log (running + recent operations).
	Operations bool
	// Files is instance file transfer: browse, download, upload.
	Files bool
	// FileDelete is instance file / empty-directory removal (Incus extension
	// "file_delete").
	FileDelete bool
	// FileMkdir is instance directory creation (Incus extension
	// "directory_manipulation").
	FileMkdir bool
	// ServerAdmin is the Server section: overview, config, certificates,
	// warnings.
	ServerAdmin bool
	// NetworkACLs is network ACL management (Incus extension "network_acl").
	NetworkACLs bool
	// VolumeBackups is custom-volume export/import (Incus extensions
	// "custom_volume_backup" + "backup_override_name" — the import names the
	// new volume, which needs the override).
	VolumeBackups bool
	// Projects is multi-tenancy support (Incus extension "projects"): the
	// project CRUD methods plus per-request scoping via WithProject.
	Projects bool
	// Events is push notification of operation changes (the daemon events
	// API): WatchOperations plus the SSE-driven Tasks panel. Without it the
	// panel falls back to polling.
	Events bool
	// Remotes is multi-server support: ListRemotes plus per-request scoping
	// via WithRemote. Only set when more than one remote is reachable.
	Remotes bool
	// Migrate is cross-remote instance migration (stopped move). Like
	// Remotes, only set when there is another reachable remote to target.
	Migrate bool
	// NetworkForwards is port-forward management on managed networks (Incus
	// extension "network_forward").
	NetworkForwards bool
	// StoredBackups is server-side named instance backups (Incus extension
	// "container_backup"): list/create/download/restore/delete without
	// streaming through the browser.
	StoredBackups bool
	// VolumeStoredBackups is server-side named backups for custom volumes
	// (Incus extension "custom_volume_backup"): list/create/download/restore-
	// as/delete. Distinct from VolumeBackups (streamed export/import, which
	// also needs "backup_override_name"). Expiry additionally needs
	// "backup_expiry".
	VolumeStoredBackups bool
	// ImageRefresh is on-demand re-pull of images from their update source
	// (Incus extension "image_force_refresh").
	ImageRefresh bool
	// CertificateEdit is rename + project restriction on trusted certificates
	// (Incus extensions "certificate_update" + "certificate_project").
	CertificateEdit bool
	// InstanceRebuild is reinstalling a stopped instance from a new image
	// while keeping its config and devices (Incus extension
	// "instances_rebuild").
	InstanceRebuild bool
	// ISOVolumes is creating custom ISO volumes from an uploaded image file,
	// attachable to VMs as install media (Incus extension
	// "custom_volume_iso").
	ISOVolumes bool
	// Hardware is the host hardware inventory on the Server page: GPU cards,
	// network cards, and physical disks (Incus extension "resources_v2").
	Hardware bool
	// ProjectUsage is per-project usage-vs-limits reporting (Incus extension
	// "project_usage"): the Usage & limits section on the project detail page
	// and the live Resources column on the projects list.
	ProjectUsage bool
	// NetworkZones is managed DNS zone + record management (Incus extension
	// "network_dns").
	NetworkZones bool
	// StorageBuckets is S3-compatible object-store bucket + access-key
	// management on storage pools (Incus extension "storage_buckets").
	StorageBuckets bool
}

// InstanceStatus is an instance's runtime state, mirroring the Incus status
// vocabulary. Only the values the UI gates behavior on are named here; the
// driver may carry through other transient daemon states (e.g. "Starting") as
// raw values for display.
type InstanceStatus string

const (
	StatusRunning InstanceStatus = "Running"
	StatusStopped InstanceStatus = "Stopped"
	StatusFrozen  InstanceStatus = "Frozen"
	StatusError   InstanceStatus = "Error"
)

// InstanceType distinguishes a system container from a virtual machine. Shared
// by Instance, Image, and CreateOptions, which all carry the same vocabulary.
type InstanceType string

const (
	TypeContainer      InstanceType = "container"
	TypeVirtualMachine InstanceType = "virtual-machine"
)

// OperationStatus is an async operation's lifecycle state, mirroring the Incus
// operation vocabulary. Only the values the UI styles are named; the driver
// carries through other daemon states as raw values for display.
type OperationStatus string

const (
	OpRunning   OperationStatus = "Running"
	OpSuccess   OperationStatus = "Success"
	OpFailure   OperationStatus = "Failure"
	OpCancelled OperationStatus = "Cancelled"
)

// WarningStatus is a server warning's acknowledgement state.
type WarningStatus string

const (
	WarningNew          WarningStatus = "new"
	WarningAcknowledged WarningStatus = "acknowledged"
	WarningResolved     WarningStatus = "resolved"
)

// Instance is a system container or virtual machine.
type Instance struct {
	Name         string
	Status       InstanceStatus
	Image        string // base image description, if known
	IPv4         []string
	Snapshots    int
	CreatedAt    time.Time
	LimitsCPU    string   // limits.cpu, e.g. "2"; empty = unset
	LimitsMemory string   // limits.memory, e.g. "2GiB"; empty = unset
	Profiles     []string // assigned profile names, in override order
	Tags         []string // from the user.tags config convention; empty = untagged
}

// ParseTags splits a user.tags config value ("web, prod") into trimmed,
// non-empty tags. Both drivers surface Instance.Tags through it so the
// convention lives in one place.
func ParseTags(raw string) []string {
	var tags []string
	for t := range strings.SplitSeq(raw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

// IsTrue reports whether a config value is truthy in the daemon's boolean
// vocabulary: Incus accepts "true", "1", "yes", and "on" (case-insensitive)
// for boolean keys, so UI state derived from config values must not compare
// against the literal "true" alone.
func IsTrue(v string) bool {
	switch strings.ToLower(v) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// Limits caps an instance's CPU and memory. Empty strings mean "leave unset"
// (and clear any existing limit on update).
type Limits struct {
	CPU    string // cores ("2") or cpuset ("0-1,3")
	Memory string // e.g. "2GiB"
}

// Profile is an Incus profile: a reusable bundle of config and devices that can
// be attached to instances. Config/Devices are read-only in this slice.
type Profile struct {
	Name        string
	Description string
	Config      map[string]string
	Devices     map[string]map[string]string // device name → {key: value}
	UsedBy      []string                     // instance names using it
	// Version is an opaque concurrency token for UpdateProfile, populated by
	// GetProfile (empty on list entries).
	Version string
}

// Network is an Incus network. Managed networks (bridges, OVN, ...) are
// configurable and deletable; unmanaged ones are host interfaces Incus only
// reports. UsedBy and Managed are read-only outputs (ignored on create); Config
// is an input applied on create and replaced on update.
type Network struct {
	Name        string
	Type        string // bridge | ovn | macvlan | physical | ...
	Managed     bool
	Status      string // daemon lifecycle state, e.g. "Created", "Pending"; "" = unknown
	Description string
	Config      map[string]string
	UsedBy      []string
	// Version is an opaque concurrency token for UpdateNetwork, populated by
	// GetNetwork (empty on list entries).
	Version string
}

// NetworkACLRule is one rule of a network ACL. Direction is carried by
// membership in NetworkACL.Ingress vs NetworkACL.Egress (the Incus API has no
// direction field). Rules are order-independent.
type NetworkACLRule struct {
	Action          string // allow | allow-stateless | reject | drop
	Source          string
	Destination     string
	Protocol        string // tcp | udp | icmp4 | icmp6 | "" (any)
	SourcePort      string
	DestinationPort string
	ICMPType        string
	ICMPCode        string
	State           string // enabled | disabled | logged
	Description     string
}

// NetworkACL is an Incus network ACL (security group). ACLs only take effect
// once attached via security.acls on a network or NIC device; attachment is
// managed outside this seam (the network config editor can set it).
type NetworkACL struct {
	Name        string
	Description string
	Ingress     []NetworkACLRule
	Egress      []NetworkACLRule
	UsedBy      []string
	// Version is an opaque concurrency token for UpdateNetworkACL, populated
	// by GetNetworkACL (empty on list entries).
	Version string
}

// StoragePool is an Incus storage pool: driver-specific infra
// (dir/zfs/btrfs/lvm/ceph). lexi supports full CRUD (Create/Update/Delete);
// Config is an input applied on create and replaced on update, while UsedBy is
// a read-only output.
type StoragePool struct {
	Name        string
	Driver      string // dir | zfs | btrfs | lvm | ceph ...
	Description string
	Config      map[string]string
	UsedBy      []string
	// SpaceUsed/SpaceTotal are the pool's disk usage in bytes, populated
	// best-effort (from the pool resources endpoint) by both ListStoragePools
	// and GetStoragePool; 0 total = unknown.
	SpaceUsed  int64
	SpaceTotal int64
	// Version is an opaque concurrency token for UpdateStoragePool, populated
	// by GetStoragePool (empty on list entries).
	Version string
}

// StorageVolume is a custom storage volume within a pool. lexi manages only the
// "custom" volume type; container/image/vm volumes are managed by their
// instances. Type/UsedBy are read-only outputs (Type is always "custom" here).
type StorageVolume struct {
	Name        string
	Type        string // always "custom" in this slice
	ContentType string // filesystem | block | iso (custom ISO volumes)
	Pool        string
	Description string
	Config      map[string]string
	UsedBy      []string
	// Version is an opaque concurrency token for UpdateVolume, populated by
	// GetVolume (empty on list entries).
	Version string
}

// StorageVolumeSnapshot is a point-in-time snapshot of a custom volume.
type StorageVolumeSnapshot struct {
	Name      string
	CreatedAt time.Time
	ExpiresAt time.Time // zero = never expires
}

// InstanceConfig is an instance's editable local config plus its devices. Config
// excludes the managed keys — volatile.*, limits.cpu/limits.memory, and
// snapshots.schedule/expiry/pattern — which are owned elsewhere and preserved
// on update. Devices is the full expanded set (read-only);
// LocalDevices is the instance-owned subset (editable).
type InstanceConfig struct {
	Config       map[string]string
	Devices      map[string]map[string]string
	LocalDevices map[string]map[string]string
	// Version is an opaque concurrency token for UpdateInstanceConfig and
	// UpdateDevice, populated by GetInstanceConfig.
	Version string
}

// Metrics is a point-in-time resource snapshot. The incus driver derives
// CPUPercent from the delta between two CPU-time samples, so it reads 0 until
// a prior sample exists (the fake returns canned values from the first call).
type Metrics struct {
	CPUPercent  float64
	MemoryUsage int64
	MemoryTotal int64
	DiskUsage   int64
	NetworkRx   int64
	NetworkTx   int64
	Processes   int64
}

// MetricSample is a Metrics snapshot stamped with the time it was taken, used
// to build the time-series history behind the metrics charts.
type MetricSample struct {
	Metrics

	Time time.Time
}

// WinSize is a terminal window size in character cells.
type WinSize struct {
	Cols int
	Rows int
}

// ExecRequest parameterizes an interactive Exec session. Stdin/Stdout bridge the
// instance PTY, Resize carries window-resize events for the lifetime of the
// session, and Width/Height seed the initial size. Command empty defaults to the
// driver's shell.
type ExecRequest struct {
	Command []string
	Stdin   io.Reader
	Stdout  io.Writer
	Resize  <-chan WinSize
	Width   int
	Height  int
}

// Snapshot is a point-in-time snapshot of an instance.
type Snapshot struct {
	Name      string
	CreatedAt time.Time
	Stateful  bool
	ExpiresAt time.Time // zero = never expires
}

// SnapshotOptions parameterizes CreateSnapshot. ExpiresAt zero = no expiry.
// Stateful captures runtime state (running instance + CRIU required; Incus
// enforces this).
type SnapshotOptions struct {
	Stateful  bool
	ExpiresAt time.Time
}

// SnapshotSchedule is an instance's auto-snapshot config (three Incus config
// keys). Empty fields mean the corresponding key is unset.
type SnapshotSchedule struct {
	Schedule string // snapshots.schedule, e.g. "@daily" or a cron expression
	Expiry   string // snapshots.expiry, e.g. "2w"
	Pattern  string // snapshots.pattern, e.g. "snap%d"
}

// Image is an entry in the create-from-image browser. The Distribution/Release/
// Variant/Type fields back the server-side search filters.
type Image struct {
	Alias        string // e.g. "debian/12"
	Fingerprint  string // exact image identity on the remote
	Description  string
	Arch         string // incus arch name, e.g. "aarch64", "x86_64"
	SizeBytes    int64
	Distribution string       // e.g. "debian"
	Release      string       // e.g. "12"
	Variant      string       // e.g. "default", "cloud"
	Type         InstanceType // "container" | "virtual-machine"
}

// ServerOverview is the host summary for the Server section: daemon and host
// identity plus the headline resources.
type ServerOverview struct {
	ServerVersion string
	Kernel        string
	KernelVersion string
	Driver        string // e.g. "lxc | qemu"
	DriverVersion string
	CPUThreads    int
	MemoryUsed    int64
	MemoryTotal   int64
}

// ServerHardware is the host hardware inventory (the daemon's /1.0/resources
// topology): GPU cards, network cards, and physical disks. The headline
// CPU/memory totals live on ServerOverview.
type ServerHardware struct {
	GPUs  []GPUCard
	NICs  []NetworkCard
	Disks []HostDisk
}

// GPUCard is a GPU device on the host.
type GPUCard struct {
	Vendor     string
	Product    string
	Driver     string // kernel driver, e.g. "i915"
	PCIAddress string
}

// NetworkCard is a physical network device on the host.
type NetworkCard struct {
	Vendor     string
	Product    string
	Driver     string
	PCIAddress string
	Ports      []NetworkPort
}

// NetworkPort is a port on a NetworkCard.
type NetworkPort struct {
	ID      string // interface name, e.g. "eth0"
	Address string // MAC address
}

// HostDisk is a physical disk on the host.
type HostDisk struct {
	ID        string // device name, e.g. "nvme0n1"
	Model     string
	Type      string // e.g. "nvme", "sata"
	SizeBytes int64
	Removable bool
}

// Certificate is one entry of the daemon's trust store.
type Certificate struct {
	Name        string
	Type        string // client | metrics | ...
	Fingerprint string
	Restricted  bool
	Projects    []string // projects the cert is limited to when Restricted
}

// Warning is a daemon warning (e.g. a config problem Incus noticed).
type Warning struct {
	UUID        string
	Type        string
	Severity    string        // low | moderate | high
	Status      WarningStatus // new | acknowledged | resolved
	Count       int
	LastMessage string
	LastSeenAt  time.Time
}

// FileEntry is one entry of an instance directory listing.
type FileEntry struct {
	Name string
	Dir  bool
	Mode string // e.g. "0644"; "" when the entry could not be statted
}

// FileInfo is instance-file metadata as reported by the driver.
type FileInfo struct {
	Type string // "file" | "directory" | "symlink"
	Mode string // e.g. "0644"
	UID  int64
	GID  int64
}

// FileWriteOptions sets ownership and mode for pushed files. The zero value
// keeps PushFile's historical behavior: root:root, mode 0644.
type FileWriteOptions struct {
	Mode string // e.g. "0644"; empty = 0644
	UID  int64
	GID  int64
}

// Operation is a daemon task: an async operation that is running or recently
// finished (Incus prunes completed operations after a few seconds).
type Operation struct {
	ID          string
	Description string
	Class       string          // task | websocket | token
	Status      OperationStatus // Running | Success | Failure | ...
	Err         string          // failure detail, "" when none
	CreatedAt   time.Time
	Cancelable  bool // the daemon will accept a cancel request for this op
}

// Project is a multi-tenancy namespace. Config carries the daemon's
// project keys — notably the features.* booleans deciding which resource
// kinds are scoped to the project rather than shared from default.
type Project struct {
	Name        string
	Description string
	Config      map[string]string
	UsedBy      []string
	// Version is an opaque concurrency token for UpdateProject, populated
	// by GetProject (empty on list entries).
	Version string
}

// StorageBucket is an S3-compatible object-store bucket on a storage pool
// (Incus extension "storage_buckets").
type StorageBucket struct {
	Name        string
	Description string
	S3URL       string // endpoint URL clients use to reach the bucket
	Size        string // the "size" config key; empty = no quota
}

// BucketKey is an access credential of a storage bucket.
type BucketKey struct {
	Name        string
	Description string
	Role        string // "admin" (read-write) or "read-only"
	AccessKey   string
	SecretKey   string
}

// NetworkZone is a managed DNS zone (Incus extension "network_dns"). The
// zone serves records for the networks that reference it via their
// dns.zone.* config keys.
type NetworkZone struct {
	Name        string // DNS zone name, e.g. "incus.example.org"
	Description string
	Config      map[string]string
	UsedBy      []string
	// Version is an opaque concurrency token for UpdateNetworkZone,
	// populated by GetNetworkZone (empty on list entries).
	Version string
}

// ZoneRecord is one record set in a network zone.
type ZoneRecord struct {
	Name        string // relative name within the zone, e.g. "www"
	Description string
	Entries     []ZoneEntry
}

// ZoneEntry is one DNS entry of a ZoneRecord.
type ZoneEntry struct {
	Type  string // e.g. "A", "AAAA", "CNAME", "TXT"
	TTL   uint64 // seconds; 0 uses the zone default
	Value string
}

// ProjectUsage is one resource row of a project's state: current usage
// against the configured limit. Memory and disk are bytes; the other
// resources are counts. Limit is -1 when the project sets none.
type ProjectUsage struct {
	Resource string // e.g. "instances", "memory"
	Usage    int64
	Limit    int64
}

// InstanceBackup is a named backup the daemon stores server-side, as opposed
// to the streamed export/import tarballs.
type InstanceBackup struct {
	Name         string
	CreatedAt    time.Time
	ExpiresAt    time.Time // zero means never auto-deleted
	InstanceOnly bool      // snapshots excluded
}

// VolumeBackup is a named backup of a custom volume the daemon stores
// server-side, as opposed to the streamed export/import tarballs.
type VolumeBackup struct {
	Name       string
	CreatedAt  time.Time
	ExpiresAt  time.Time // zero means never auto-deleted
	VolumeOnly bool      // volume snapshots excluded
}

// NetworkForward is a port forward on a managed network: traffic to the
// listen address (and its port mappings) is redirected into instances.
type NetworkForward struct {
	ListenAddress string
	Description   string
	// DefaultTarget receives traffic for unmapped ports when set (the
	// daemon's target_address config key).
	DefaultTarget string
	Ports         []ForwardPort
}

// ForwardPort is one port mapping in a forward. Port fields carry the
// daemon's comma-and-range syntax (e.g. "80,8080-8090") verbatim.
type ForwardPort struct {
	Description   string
	Protocol      string // "tcp" or "udp"
	ListenPort    string
	TargetAddress string
	TargetPort    string // empty means same as ListenPort
}

// NetworkLease is one DHCP lease on a managed network.
type NetworkLease struct {
	Hostname string
	MAC      string
	Address  string
	Type     string // "dynamic", "static", or "gateway"
}

// NetworkState is the live interface state of a managed network.
type NetworkState struct {
	State     string // e.g. "up"
	MTU       int
	Addresses []string
}

// Remote is a configured Incus server lexi can scope requests to. The list
// is read from the CLI config; lexi does not add or trust remotes itself.
type Remote struct {
	Name    string
	Addr    string
	Current bool // selected by the request context, or the default remote
}

// remoteKey is the context key WithRemote stores the selection under.
type remoteKey struct{}

// WithRemote returns a context whose backend calls are scoped to the named
// remote, when the driver supports remotes (Capabilities.Remotes).
func WithRemote(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, remoteKey{}, name)
}

// RemoteFromContext reports the remote a request is scoped to; "" means the
// default remote (also for contexts that never saw WithRemote).
func RemoteFromContext(ctx context.Context) string {
	if name, ok := ctx.Value(remoteKey{}).(string); ok {
		return name
	}
	return ""
}

// projectKey is the context key WithProject stores the selection under.
type projectKey struct{}

// WithProject returns a context whose backend calls are scoped to the named
// project, when the driver supports projects (Capabilities.Projects).
func WithProject(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, projectKey{}, name)
}

// ProjectFromContext reports the project a request is scoped to; "" means
// the default project (also for contexts that never saw WithProject).
func ProjectFromContext(ctx context.Context) string {
	if name, ok := ctx.Value(projectKey{}).(string); ok {
		return name
	}
	return ""
}

// LocalImage is an image in the host's local image store (as opposed to Image,
// which is a per-alias entry of the remote catalog backing the create picker).
type LocalImage struct {
	Fingerprint string
	Aliases     []string
	Description string
	Arch        string // incus arch name, e.g. "aarch64", "x86_64"
	SizeBytes   int64
	Type        InstanceType // "container" | "virtual-machine"
	CreatedAt   time.Time
	Public      bool      // visible to unauthenticated clients
	AutoUpdate  bool      // daemon re-pulls from the update source
	ExpiresAt   time.Time // zero means never expires
	// HasUpdateSource marks images that can refresh (copied from a remote);
	// locally published or imported images have nothing to re-pull from.
	HasUpdateSource bool
}

// ImageEdit is the whole-object image edit: every field is applied (the
// daemon's ImagePut semantics).
type ImageEdit struct {
	Description string
	Public      bool
	AutoUpdate  bool
	ExpiresAt   time.Time // zero clears the expiry
}

// CreateOptions parameterizes CreateInstance. The zero value of every optional
// field preserves the plain create: default profile, profile-supplied root
// disk and network, no extra config.
type CreateOptions struct {
	Name        string
	Image       string       // display alias on the images remote
	Fingerprint string       // exact image identity; empty falls back to Image
	Type        InstanceType // "container" | "virtual-machine"; empty defaults to container
	Start       bool
	// Profiles to apply in order (later override earlier); empty keeps the
	// daemon's default ([default]).
	Profiles []string
	// Pool overrides the root disk's storage pool via a local "root" device,
	// shadowing any profile-supplied root disk (mirrors `incus create -s`).
	Pool string
	// Network attaches a managed network via a local "eth0" nic device
	// (mirrors `incus create -n`).
	Network string
	// Config seeds initial instance config keys (limits, cloud-init, ...).
	Config map[string]string
}

// Backend is the single seam between the HTTP layer and a container driver.
//
// Project scoping: when Capabilities.Projects is set, drivers honor the
// project carried by the request context (WithProject/ProjectFromContext) on
// every project-scoped method; an unset project means the default project.
// Drivers without project support ignore it.
type Backend interface {
	// Capabilities reports the feature flags for the daemon the request is
	// scoped to. Flags are probed per server (extensions differ between
	// daemons), so the UI never offers an operation the connected daemon
	// doesn't support.
	Capabilities(ctx context.Context) Capabilities

	ListInstances(ctx context.Context) ([]Instance, error)
	GetInstance(ctx context.Context, name string) (Instance, error)
	CreateInstance(ctx context.Context, opt CreateOptions) error
	StartInstance(ctx context.Context, name string) error
	StopInstance(ctx context.Context, name string) error
	RestartInstance(ctx context.Context, name string) error
	PauseInstance(ctx context.Context, name string) error  // freeze
	ResumeInstance(ctx context.Context, name string) error // unfreeze
	DeleteInstance(ctx context.Context, name string) error // stop-then-delete
	// RebuildInstance reinstalls a stopped instance from the catalog image
	// behind image/fingerprint (fingerprint wins when set, like create),
	// keeping the instance's config, devices, and profiles. A running
	// instance or empty image is ErrInvalid.
	RebuildInstance(ctx context.Context, name, image, fingerprint string) error

	ListSnapshots(ctx context.Context, name string) ([]Snapshot, error)
	CreateSnapshot(ctx context.Context, name, snapshot string, opts SnapshotOptions) error
	RenameSnapshot(ctx context.Context, name, snapshot, newName string) error
	UpdateSnapshotExpiry(ctx context.Context, name, snapshot string, expiresAt time.Time) error
	RestoreSnapshot(ctx context.Context, name, snapshot string) error
	DeleteSnapshot(ctx context.Context, name, snapshot string) error
	GetSnapshotSchedule(ctx context.Context, name string) (SnapshotSchedule, error)
	SetSnapshotSchedule(ctx context.Context, name string, s SnapshotSchedule) error

	CloneInstance(ctx context.Context, src, dst string) error
	RenameInstance(ctx context.Context, name, newName string) error
	MoveInstance(ctx context.Context, name, pool string) error

	UpdateLimits(ctx context.Context, name string, l Limits) error
	Metrics(ctx context.Context, name string) (Metrics, error)

	ListProfiles(ctx context.Context) ([]Profile, error)
	GetProfile(ctx context.Context, name string) (Profile, error)
	CreateProfile(ctx context.Context, name, description string) error
	// UpdateProfile updates the profile's description and replaces its config
	// map, preserving its devices untouched. A non-empty version (from
	// GetProfile) makes the update conditional: ErrConflict if the profile
	// changed since that read.
	UpdateProfile(ctx context.Context, name, description string, config map[string]string, version string) error
	// DeleteProfile removes an unused profile. "default" is undeletable
	// (ErrInvalid); a profile still used by instances is ErrConflict.
	DeleteProfile(ctx context.Context, name string) error
	// RenameProfile renames a profile. "default" cannot be renamed (ErrInvalid);
	// the target name must be free (ErrConflict).
	RenameProfile(ctx context.Context, name, newName string) error
	// AddProfileDevice attaches (or overwrites) a device on the profile.
	AddProfileDevice(ctx context.Context, profile, device string, config map[string]string) error
	// UpdateProfileDevice replaces the named device's config map. The device
	// must exist (ErrNotFound). A non-empty version (from GetProfile) makes the
	// update conditional: ErrConflict if the profile changed since that read.
	UpdateProfileDevice(ctx context.Context, profile, device string, config map[string]string, version string) error
	// RemoveProfileDevice detaches a device. The device must exist (ErrNotFound).
	RemoveProfileDevice(ctx context.Context, profile, device string) error
	// SetInstanceProfiles replaces the instance's profile list (ordered; later
	// profiles override earlier ones).
	SetInstanceProfiles(ctx context.Context, name string, profiles []string) error

	GetInstanceConfig(ctx context.Context, name string) (InstanceConfig, error)
	// UpdateInstanceConfig replaces the editable config wholesale. A non-empty
	// version (from GetInstanceConfig) makes the update conditional:
	// ErrConflict if the instance changed since that read, so two editors
	// can't silently clobber each other (same contract as UpdateDevice).
	UpdateInstanceConfig(ctx context.Context, name string, config map[string]string, version string) error
	// AddDevice attaches (or overwrites) a local device on the instance.
	AddDevice(ctx context.Context, name, device string, config map[string]string) error
	// UpdateDevice replaces the named local device's config map. The device
	// must exist (ErrNotFound). A non-empty version (from GetInstanceConfig)
	// makes the update conditional: ErrConflict if the instance changed since
	// that read.
	UpdateDevice(ctx context.Context, name, device string, config map[string]string, version string) error
	// RemoveDevice detaches a local device. The device must exist (ErrNotFound).
	RemoveDevice(ctx context.Context, name, device string) error

	ListNetworks(ctx context.Context) ([]Network, error)
	GetNetwork(ctx context.Context, name string) (Network, error)
	CreateNetwork(ctx context.Context, n Network) error
	// UpdateNetwork updates a managed network's description and replaces its
	// config map. A non-empty version (from GetNetwork) makes the update
	// conditional: ErrConflict if the network changed since that read; empty
	// updates unconditionally.
	UpdateNetwork(ctx context.Context, name, description string, config map[string]string, version string) error
	DeleteNetwork(ctx context.Context, name string) error

	ListNetworkACLs(ctx context.Context) ([]NetworkACL, error)
	GetNetworkACL(ctx context.Context, name string) (NetworkACL, error)
	// CreateNetworkACL makes an empty ACL from acl.Name and acl.Description;
	// rules are added later via UpdateNetworkACL, so Ingress/Egress on acl are
	// ignored here (create is intentionally kept to name + description, even
	// though the daemon API would accept rules in the same call).
	CreateNetworkACL(ctx context.Context, acl NetworkACL) error
	// UpdateNetworkACL replaces the ACL's description and both rule lists. A
	// non-empty version (from GetNetworkACL) makes the update conditional:
	// ErrConflict if the ACL changed since that read.
	UpdateNetworkACL(ctx context.Context, name, description string, ingress, egress []NetworkACLRule, version string) error
	// RenameNetworkACL renames an ACL; the target name must be free
	// (ErrConflict).
	RenameNetworkACL(ctx context.Context, name, newName string) error
	// DeleteNetworkACL refuses ACLs that are in use (ErrConflict).
	DeleteNetworkACL(ctx context.Context, name string) error

	// ListNetworkLeases reports the DHCP leases of a managed network.
	ListNetworkLeases(ctx context.Context, network string) ([]NetworkLease, error)
	// GetNetworkState reports a managed network's live interface state.
	GetNetworkState(ctx context.Context, network string) (NetworkState, error)
	// ListNetworkForwards lists a managed network's port forwards.
	ListNetworkForwards(ctx context.Context, network string) ([]NetworkForward, error)
	// CreateNetworkForward adds a forward; the listen address is its identity
	// (a duplicate is ErrConflict).
	CreateNetworkForward(ctx context.Context, network string, f NetworkForward) error
	// UpdateNetworkForward replaces a forward's description, default target,
	// and port set. Deliberately unversioned — the daemon enforces no etag
	// on forwards, so last write wins.
	UpdateNetworkForward(ctx context.Context, network string, f NetworkForward) error
	// DeleteNetworkForward removes the forward at the listen address.
	DeleteNetworkForward(ctx context.Context, network, listenAddress string) error

	ListNetworkZones(ctx context.Context) ([]NetworkZone, error)
	// GetNetworkZone returns the zone with a populated Version token.
	GetNetworkZone(ctx context.Context, name string) (NetworkZone, error)
	// CreateNetworkZone creates a DNS zone from zone.Name and
	// zone.Description; the name must be a valid zone name (ErrInvalid) and
	// free (ErrConflict). Config is set later via UpdateNetworkZone.
	CreateNetworkZone(ctx context.Context, zone NetworkZone) error
	// UpdateNetworkZone replaces the zone's description and config,
	// conditionally on version (ErrConflict when stale; empty version is
	// unconditional).
	UpdateNetworkZone(ctx context.Context, name, description string, config map[string]string, version string) error
	// DeleteNetworkZone refuses zones referenced by a network (ErrConflict).
	DeleteNetworkZone(ctx context.Context, name string) error
	// ListZoneRecords lists the zone's record sets, sorted by name.
	ListZoneRecords(ctx context.Context, zone string) ([]ZoneRecord, error)
	// CreateZoneRecord adds a record set; the name is its identity within
	// the zone (a duplicate is ErrConflict) and every entry needs a type
	// and value (ErrInvalid).
	CreateZoneRecord(ctx context.Context, zone string, r ZoneRecord) error
	// DeleteZoneRecord removes the named record set from the zone.
	DeleteZoneRecord(ctx context.Context, zone, name string) error

	// ListRemotes returns the reachable configured remotes, marking the one
	// the request context selects (or the default) as Current.
	ListRemotes(ctx context.Context) ([]Remote, error)
	// MigrateInstance moves a stopped instance to another reachable remote
	// (copy, then delete the source), landing in the target's default
	// project. An empty newName keeps the name. A running instance is
	// ErrInvalid, an unknown target ErrNotFound, a taken target name
	// ErrConflict (the source survives any failure).
	MigrateInstance(ctx context.Context, name, targetRemote, newName string) error

	ListProjects(ctx context.Context) ([]Project, error)
	// GetProject returns the project with a populated Version token.
	GetProject(ctx context.Context, name string) (Project, error)
	// GetProjectUsage reports the project's resource usage against its
	// configured limits, sorted by resource name. An unknown project is
	// ErrNotFound.
	GetProjectUsage(ctx context.Context, name string) ([]ProjectUsage, error)
	// CreateProject creates a project from project.Name, project.Description,
	// and project.Config; Config carries the features.* keys (the daemon
	// defaults unset features for new projects).
	CreateProject(ctx context.Context, project Project) error
	// UpdateProject replaces the project's description and config,
	// conditionally on version (ErrConflict when stale; empty version is
	// unconditional).
	UpdateProject(ctx context.Context, name, description string, config map[string]string, version string) error
	// RenameProject renames a project; the default project is refused
	// (ErrInvalid) and the target name must be free (ErrConflict).
	RenameProject(ctx context.Context, name, newName string) error
	// DeleteProject refuses the default project (ErrInvalid) and non-empty
	// projects (ErrConflict).
	DeleteProject(ctx context.Context, name string) error

	ListStoragePools(ctx context.Context) ([]StoragePool, error)
	GetStoragePool(ctx context.Context, pool string) (StoragePool, error)
	// CreateStoragePool creates a pool from Name, Driver, Description, and
	// Config (UsedBy is a read-only output).
	CreateStoragePool(ctx context.Context, p StoragePool) error
	// UpdateStoragePool updates the pool's description and replaces its config
	// map. A non-empty version (from GetStoragePool) makes the update
	// conditional: ErrConflict if the pool changed since that read. Some pool
	// config keys are immutable post-create; the daemon rejects those with
	// ErrInvalid.
	UpdateStoragePool(ctx context.Context, name, description string, config map[string]string, version string) error
	// DeleteStoragePool refuses pools with UsedBy references (ErrConflict).
	DeleteStoragePool(ctx context.Context, name string) error
	ListVolumes(ctx context.Context, pool string) ([]StorageVolume, error)
	GetVolume(ctx context.Context, pool, name string) (StorageVolume, error)
	CreateVolume(ctx context.Context, pool string, v StorageVolume) error
	// UpdateVolume updates the volume's description and replaces its config map
	// (resizing = the "size" key). A non-empty version (from GetVolume) makes
	// the update conditional: ErrConflict if the volume's config changed since
	// that read. Caveat: the Incus volume etag covers name/type/config but not
	// description, so concurrent description-only edits are last-write-wins.
	UpdateVolume(ctx context.Context, pool, name, description string, config map[string]string, version string) error
	// RenameVolume renames a custom volume. The target name must be free
	// (ErrConflict); a volume in use by an instance is refused by the daemon.
	RenameVolume(ctx context.Context, pool, name, newName string) error
	DeleteVolume(ctx context.Context, pool, name string) error
	ListVolumeSnapshots(ctx context.Context, pool, volume string) ([]StorageVolumeSnapshot, error)
	CreateVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error
	RestoreVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error
	// RenameVolumeSnapshot renames a custom-volume snapshot. The target name
	// must be free (ErrConflict) and the snapshot must exist (ErrNotFound).
	RenameVolumeSnapshot(ctx context.Context, pool, volume, snapshot, newName string) error
	// UpdateVolumeSnapshotExpiry sets a custom-volume snapshot's expiry; a zero
	// time clears it.
	UpdateVolumeSnapshotExpiry(ctx context.Context, pool, volume, snapshot string, expiresAt time.Time) error
	DeleteVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error

	// CreateVolumeFromISO creates custom volume volume in pool (content type
	// "iso") from an ISO image read from r. The name must be free
	// (ErrConflict) and the pool must exist (ErrNotFound).
	CreateVolumeFromISO(ctx context.Context, pool, volume string, r io.Reader) error

	// ListBuckets lists the pool's storage buckets, sorted by name.
	ListBuckets(ctx context.Context, pool string) ([]StorageBucket, error)
	// CreateBucket creates a bucket in pool from bucket.Name,
	// bucket.Description, and bucket.Size (a non-empty size like "100MiB"
	// caps it); S3URL is an output and ignored here. The daemon seeds an
	// initial admin key, visible via ListBucketKeys. The name must be free
	// (ErrConflict) and the pool must exist (ErrNotFound).
	CreateBucket(ctx context.Context, pool string, bucket StorageBucket) error
	// DeleteBucket removes the bucket and its keys.
	DeleteBucket(ctx context.Context, pool, name string) error
	// ListBucketKeys lists the bucket's access keys (credentials included),
	// sorted by name.
	ListBucketKeys(ctx context.Context, pool, bucket string) ([]BucketKey, error)
	// CreateBucketKey adds a credential and returns it with the generated
	// access/secret keys. Role must be "admin" or "read-only"; empty
	// defaults to read-only.
	CreateBucketKey(ctx context.Context, pool, bucket, name, description, role string) (BucketKey, error)
	// DeleteBucketKey revokes the named credential.
	DeleteBucketKey(ctx context.Context, pool, bucket, name string) error

	// ExportVolume streams a portable backup tarball of the custom volume
	// (snapshots included) to w.
	ExportVolume(ctx context.Context, pool, volume string, w io.Writer) error
	// ImportVolume creates custom volume volume in pool from a backup tarball
	// read from r (as produced by ExportVolume). The name must be free
	// (ErrConflict) and the pool must exist (ErrNotFound).
	ImportVolume(ctx context.Context, pool, volume string, r io.Reader) error

	// ExportInstance streams a portable backup tarball of the instance to w.
	ExportInstance(ctx context.Context, name string, w io.Writer) error
	// ImportInstance creates an instance named name from a backup tarball read
	// from r (as produced by ExportInstance).
	ImportInstance(ctx context.Context, name string, r io.Reader) error

	// ListInstanceBackups lists the named backups stored on the server for
	// an instance, oldest first.
	ListInstanceBackups(ctx context.Context, instance string) ([]InstanceBackup, error)
	// CreateInstanceBackup stores a server-side backup. An empty name gets
	// the daemon-style backupN default; a zero expiresAt never auto-deletes;
	// instanceOnly skips snapshots. A non-zero expiry on a daemon without
	// the backup_expiry extension is ErrUnsupported.
	CreateInstanceBackup(ctx context.Context, instance, name string, expiresAt time.Time, instanceOnly bool) error
	// DeleteInstanceBackup removes a stored backup.
	DeleteInstanceBackup(ctx context.Context, instance, backup string) error
	// ExportInstanceBackup streams a stored backup's tarball to w.
	ExportInstanceBackup(ctx context.Context, instance, backup string, w io.Writer) error
	// RestoreInstanceBackup creates a new instance from a stored backup,
	// entirely server-side. A taken newName is ErrConflict.
	RestoreInstanceBackup(ctx context.Context, instance, backup, newName string) error

	// ListVolumeBackups lists the named backups stored on the server for the
	// custom volume, oldest first.
	ListVolumeBackups(ctx context.Context, pool, volume string) ([]VolumeBackup, error)
	// CreateVolumeBackup stores a server-side backup. An empty name gets the
	// daemon-style backupN default; a zero expiresAt never auto-deletes;
	// volumeOnly excludes volume snapshots. A non-zero expiry on a daemon
	// without the backup_expiry extension is ErrUnsupported.
	CreateVolumeBackup(ctx context.Context, pool, volume, name string, expiresAt time.Time, volumeOnly bool) error
	// DeleteVolumeBackup removes a stored backup.
	DeleteVolumeBackup(ctx context.Context, pool, volume, backup string) error
	// ExportVolumeBackup streams a stored backup's tarball to w.
	ExportVolumeBackup(ctx context.Context, pool, volume, backup string, w io.Writer) error
	// RestoreVolumeBackup creates a new custom volume named newName in
	// targetPool from a stored backup. A taken newName is ErrConflict.
	RestoreVolumeBackup(ctx context.Context, pool, volume, backup, targetPool, newName string) error

	// ConsoleLog returns the instance's console log output.
	ConsoleLog(ctx context.Context, name string) (string, error)
	// Exec runs an interactive command (the driver's default shell when
	// req.Command is empty), bridging req.Stdin/Stdout to the instance PTY and
	// applying window resizes from req.Resize until the session ends.
	Exec(ctx context.Context, name string, req ExecRequest) error

	ListImages(ctx context.Context) ([]Image, error) // for the create dropdown

	ListLocalImages(ctx context.Context) ([]LocalImage, error)
	// PublishImage creates a local image from the instance, tagged with alias
	// when non-empty. Best done while the instance is stopped — publishing a
	// running one captures a live, possibly-inconsistent rootfs — but neither
	// lexi nor the daemon's image handler enforces that.
	PublishImage(ctx context.Context, instance, alias string) error
	// CopyImage pulls the image behind alias from the images remote into the
	// local store, copying its aliases.
	CopyImage(ctx context.Context, alias string) error
	DeleteImage(ctx context.Context, fingerprint string) error
	AddImageAlias(ctx context.Context, fingerprint, alias string) error
	RemoveImageAlias(ctx context.Context, alias string) error
	// ExportImage spools the image and returns its download filename plus a
	// reader over the result: a single tarball (the daemon-reported name,
	// e.g. "<fingerprint>.tar.gz") or, for split images (separate metadata +
	// rootfs, typically VM images), a "<fingerprint>.zip" with a "metadata"
	// entry plus "rootfs" (container) or "rootfs.img" (VM). The filename is
	// known before any payload byte so callers can set response headers;
	// Close releases the spool.
	ExportImage(ctx context.Context, fingerprint string) (string, io.ReadCloser, error)
	// ImportImage creates a local image from r — either a unified tarball or
	// a split-zip as produced by ExportImage (detected by the zip signature;
	// the rootfs entry name carries the image type) — tagging it with alias
	// when non-empty (a failed alias rolls the import back, like
	// PublishImage).
	ImportImage(ctx context.Context, r io.Reader, alias string) error
	// UpdateImage applies every ImageEdit field — description, public,
	// auto-update, and expiry — preserving the image's other properties and
	// flags (GET-preserve-PUT). The read-then-write is conditional on the
	// image's ETag, so a concurrent edit in the GET→PUT window fails with
	// ErrConflict rather than silently clobbering it.
	UpdateImage(ctx context.Context, fingerprint string, edit ImageEdit) error
	// RefreshImage re-pulls an image from its update source (Incus extension
	// "image_force_refresh"). An image without one — e.g. published locally —
	// is ErrInvalid.
	RefreshImage(ctx context.Context, fingerprint string) error

	// ListOperations returns running and recently finished daemon tasks,
	// newest first.
	ListOperations(ctx context.Context) ([]Operation, error)
	// CancelOperation cancels a running, cancelable operation. An unknown id is
	// ErrNotFound; an operation the daemon won't cancel is ErrInvalid.
	CancelOperation(ctx context.Context, id string) error
	// WatchOperations reports daemon operation changes as coalesced ticks.
	// The channel is closed when ctx ends. Callers re-list on each tick; no
	// event payload crosses the seam.
	WatchOperations(ctx context.Context) (<-chan struct{}, error)

	// ListFiles lists the instance directory at path (absolute), directories
	// first. Listing a file is ErrInvalid.
	ListFiles(ctx context.Context, instance, path string) ([]FileEntry, error)
	// PullFile streams the instance file at path to w. Pulling a directory is
	// ErrInvalid.
	PullFile(ctx context.Context, instance, path string, w io.Writer) error
	// PushFile creates (or overwrites) the instance file at path from r. The
	// ownership and mode options are always applied — on create and on
	// overwrite — so the zero value (root:root, 0644) resets an overwritten
	// file's owner and mode to those defaults rather than preserving them.
	PushFile(ctx context.Context, instance, path string, r io.Reader, opts FileWriteOptions) error
	// PullFileInfo streams the file at path to w like PullFile but also
	// returns its metadata. A limit > 0 caps the read: larger files fail with
	// ErrInvalid without streaming the remainder. Directories and symlinks
	// report their type without content.
	PullFileInfo(ctx context.Context, instance, path string, w io.Writer, limit int64) (FileInfo, error)
	// PullFileHead streams up to limit bytes of the file at path to w and
	// reports its metadata plus whether the file was longer than limit
	// (truncated). Unlike PullFileInfo it never rejects large files: the
	// read-only viewer shows the head plus a truncation notice. Directories and
	// symlinks report their type without content. limit must be > 0.
	PullFileHead(ctx context.Context, instance, path string, w io.Writer, limit int64) (info FileInfo, truncated bool, err error)
	// DeleteFile removes the instance file at path; directories must be empty
	// (the daemon API is non-recursive). Deleting "/" is ErrInvalid.
	DeleteFile(ctx context.Context, instance, path string) error
	// MakeDirectory creates a directory at path (parents must exist).
	MakeDirectory(ctx context.Context, instance, path string) error

	GetServerOverview(ctx context.Context) (ServerOverview, error)
	// GetServerHardware returns the host hardware inventory: GPU cards,
	// network cards, and physical disks.
	GetServerHardware(ctx context.Context) (ServerHardware, error)
	// GetServerConfig returns the server config map plus an opaque version
	// token for optimistic concurrency on update.
	GetServerConfig(ctx context.Context) (map[string]string, string, error)
	// UpdateServerConfig replaces the server's config map. A non-empty version
	// (from GetServerConfig) makes the replace conditional: if the config
	// changed since that read, the update fails with ErrConflict instead of
	// silently overwriting the concurrent change. An empty version updates
	// unconditionally.
	UpdateServerConfig(ctx context.Context, config map[string]string, version string) error
	ListCertificates(ctx context.Context) ([]Certificate, error)
	// AddCertificate adds a PEM-encoded certificate to the daemon's trust
	// store. Data that isn't a PEM CERTIFICATE block is ErrInvalid; the daemon
	// is authoritative for X.509 validity and the certificate type.
	AddCertificate(ctx context.Context, name, certType, pemData string) error
	// DeleteCertificate removes a certificate from the trust store by its
	// fingerprint. An unknown fingerprint is ErrNotFound.
	DeleteCertificate(ctx context.Context, fingerprint string) error
	// UpdateCertificate renames a trusted certificate and sets its project
	// restriction. When restricted, the cert is limited to the given projects.
	// An empty name is ErrInvalid; an unknown fingerprint is ErrNotFound.
	UpdateCertificate(ctx context.Context, fingerprint, name string, restricted bool, projects []string) error
	// ListWarnings returns daemon warnings, newest last-seen first.
	ListWarnings(ctx context.Context) ([]Warning, error)
	DeleteWarning(ctx context.Context, uuid string) error
	// AcknowledgeWarning marks a warning as acknowledged without removing it.
	AcknowledgeWarning(ctx context.Context, uuid string) error
}
