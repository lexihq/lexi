// Package backend defines the driver-agnostic contract lxcon drives the UI
// through. Domain types are intentionally decoupled from incus/shared/api so a
// future liblxc driver can implement the same interface without a rewrite.
package backend

import (
	"context"
	"errors"
	"io"
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
	Backup     bool // false in v1
	Console    bool // false in v1
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
}

// Instance is a system container or virtual machine.
type Instance struct {
	Name         string
	Status       string // Running | Stopped | ...
	Image        string // base image description, if known
	IPv4         []string
	Snapshots    int
	CreatedAt    time.Time
	LimitsCPU    string   // limits.cpu, e.g. "2"; empty = unset
	LimitsMemory string   // limits.memory, e.g. "2GiB"; empty = unset
	Profiles     []string // assigned profile names, in override order
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
// reports. Config/UsedBy/Managed are read-only outputs (ignored on create).
type Network struct {
	Name        string
	Type        string // bridge | ovn | macvlan | physical | ...
	Managed     bool
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

// StoragePool is an Incus storage pool. Pools are driver-specific infra
// (dir/zfs/btrfs/lvm/ceph) created at host setup; lxcon lists them read-only.
// Config/UsedBy are read-only outputs.
type StoragePool struct {
	Name        string
	Driver      string // dir | zfs | btrfs | lvm | ceph ...
	Description string
	Config      map[string]string
	UsedBy      []string
	// Version is an opaque concurrency token for UpdateStoragePool, populated
	// by GetStoragePool (empty on list entries).
	Version string
}

// StorageVolume is a custom storage volume within a pool. lxcon manages only the
// "custom" volume type; container/image/vm volumes are managed by their
// instances. Type/UsedBy are read-only outputs (Type is always "custom" here).
type StorageVolume struct {
	Name        string
	Type        string // always "custom" in this slice
	ContentType string // filesystem | block
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
// excludes volatile.* and limits.cpu/limits.memory, which are managed elsewhere
// and preserved on update. Devices is the full expanded set (read-only);
// LocalDevices is the instance-owned subset (editable).
type InstanceConfig struct {
	Config       map[string]string
	Devices      map[string]map[string]string
	LocalDevices map[string]map[string]string
	// Version is an opaque concurrency token for UpdateDevice, populated by
	// GetInstanceConfig.
	Version string
}

// Metrics is a point-in-time resource snapshot. CPUPercent is derived from the
// delta between two CPU-time samples, so it reads 0 until a prior sample exists.
type Metrics struct {
	CPUPercent  float64
	MemoryUsage int64
	MemoryTotal int64
	DiskUsage   int64
	NetworkRx   int64
	NetworkTx   int64
	Processes   int64
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
	Distribution string // e.g. "debian"
	Release      string // e.g. "12"
	Variant      string // e.g. "default", "cloud"
	Type         string // "container" | "virtual-machine"
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
	Severity    string // low | moderate | high
	Status      string // new | acknowledged | resolved
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
	Class       string // task | websocket | token
	Status      string // Running | Success | Failure | ...
	Err         string // failure detail, "" when none
	CreatedAt   time.Time
	Cancelable  bool // the daemon will accept a cancel request for this op
}

// LocalImage is an image in the host's local image store (as opposed to Image,
// which is a per-alias entry of the remote catalog backing the create picker).
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

// InstanceBackup is a named backup the daemon stores server-side, as opposed
// to the streamed export/import tarballs.
type InstanceBackup struct {
	Name         string
	CreatedAt    time.Time
	ExpiresAt    time.Time // zero means never auto-deleted
	InstanceOnly bool      // snapshots excluded
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

// Remote is a configured Incus server lxcon can scope requests to. The list
// is read from the CLI config; lxcon does not add or trust remotes itself.
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

type LocalImage struct {
	Fingerprint string
	Aliases     []string
	Description string
	Arch        string // incus arch name, e.g. "aarch64", "x86_64"
	SizeBytes   int64
	Type        string // "container" | "virtual-machine"
	CreatedAt   time.Time
	Public      bool      // visible to unauthenticated clients
	AutoUpdate  bool      // daemon re-pulls from the update source
	ExpiresAt   time.Time // zero means never expires
	// HasUpdateSource marks images that can refresh (copied from a remote);
	// locally published or imported images have nothing to re-pull from.
	HasUpdateSource bool
}

// ImageEdit is the whole-object image edit: every field is applied (the
// daemon's ImagePut semantics). Deliberately unversioned — last write wins.
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
	Image       string // display alias on the images remote
	Fingerprint string // exact image identity; empty falls back to Image
	Type        string // "container" | "virtual-machine"; empty defaults to container
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
	UpdateInstanceConfig(ctx context.Context, name string, config map[string]string) error
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
	CreateNetworkACL(ctx context.Context, name, description string) error
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
	// on forwards, so last write wins (like UpdateImage).
	UpdateNetworkForward(ctx context.Context, network string, f NetworkForward) error
	// DeleteNetworkForward removes the forward at the listen address.
	DeleteNetworkForward(ctx context.Context, network, listenAddress string) error

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
	// CreateProject creates a project; config carries the features.* keys
	// (the daemon defaults unset features for new projects).
	CreateProject(ctx context.Context, name, description string, config map[string]string) error
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

	// ConsoleLog returns the instance's console log output.
	ConsoleLog(ctx context.Context, name string) (string, error)
	// Exec runs an interactive command (the driver's default shell when
	// req.Command is empty), bridging req.Stdin/Stdout to the instance PTY and
	// applying window resizes from req.Resize until the session ends.
	Exec(ctx context.Context, name string, req ExecRequest) error

	ListImages(ctx context.Context) ([]Image, error) // for the create dropdown

	ListLocalImages(ctx context.Context) ([]LocalImage, error)
	// PublishImage creates a local image from the (stopped) instance, tagged
	// with alias when non-empty.
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
	// UpdateImage sets the image's description and public visibility,
	// preserving its other properties and flags (GET-preserve-PUT; the small
	// two-field edit is deliberately unversioned — last write wins).
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
	// ownership and mode options apply only when the file is created (zero
	// value: root:root 0644); overwriting keeps the existing file's metadata,
	// matching the Incus file API.
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
