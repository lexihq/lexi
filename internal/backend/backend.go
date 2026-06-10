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

// StoragePool is an Incus storage pool. Pools are driver-specific infra
// (dir/zfs/btrfs/lvm/ceph) created at host setup; lxcon lists them read-only.
// Config/UsedBy are read-only outputs.
type StoragePool struct {
	Name        string
	Driver      string // dir | zfs | btrfs | lvm | ceph ...
	Description string
	Config      map[string]string
	UsedBy      []string
}

// StorageVolume is a custom storage volume within a pool. lxcon manages only the
// "custom" volume type; container/image/vm volumes are managed by their
// instances. Type/UsedBy are read-only outputs (Type is always "custom" here).
type StorageVolume struct {
	Name        string
	Type        string // always "custom" in this slice
	ContentType string // filesystem | block
	Pool        string
	Config      map[string]string
	UsedBy      []string
}

// StorageVolumeSnapshot is a point-in-time snapshot of a custom volume.
type StorageVolumeSnapshot struct {
	Name      string
	CreatedAt time.Time
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

// Certificate is one entry of the daemon's trust store.
type Certificate struct {
	Name        string
	Type        string // client | metrics | ...
	Fingerprint string
	Restricted  bool
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
}

// LocalImage is an image in the host's local image store (as opposed to Image,
// which is a per-alias entry of the remote catalog backing the create picker).
type LocalImage struct {
	Fingerprint string
	Aliases     []string
	Description string
	Arch        string // incus arch name, e.g. "aarch64", "x86_64"
	SizeBytes   int64
	Type        string // "container" | "virtual-machine"
	CreatedAt   time.Time
}

// CreateOptions parameterizes CreateInstance.
type CreateOptions struct {
	Name        string
	Image       string // display alias on the images remote
	Fingerprint string // exact image identity; empty falls back to Image
	Type        string // "container" | "virtual-machine"; empty defaults to container
	Start       bool
}

// Backend is the single seam between the HTTP layer and a container driver.
type Backend interface {
	Capabilities() Capabilities

	ListInstances(ctx context.Context) ([]Instance, error)
	GetInstance(ctx context.Context, name string) (Instance, error)
	CreateInstance(ctx context.Context, opt CreateOptions) error
	StartInstance(ctx context.Context, name string) error
	StopInstance(ctx context.Context, name string) error
	RestartInstance(ctx context.Context, name string) error
	PauseInstance(ctx context.Context, name string) error  // freeze
	ResumeInstance(ctx context.Context, name string) error // unfreeze
	DeleteInstance(ctx context.Context, name string) error // stop-then-delete

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

	ListStoragePools(ctx context.Context) ([]StoragePool, error)
	GetStoragePool(ctx context.Context, pool string) (StoragePool, error)
	// CreateStoragePool creates a pool from Name, Driver, Description, and
	// Config (UsedBy is a read-only output).
	CreateStoragePool(ctx context.Context, p StoragePool) error
	// DeleteStoragePool refuses pools with UsedBy references (ErrConflict).
	DeleteStoragePool(ctx context.Context, name string) error
	ListVolumes(ctx context.Context, pool string) ([]StorageVolume, error)
	GetVolume(ctx context.Context, pool, name string) (StorageVolume, error)
	CreateVolume(ctx context.Context, pool string, v StorageVolume) error
	DeleteVolume(ctx context.Context, pool, name string) error
	ListVolumeSnapshots(ctx context.Context, pool, volume string) ([]StorageVolumeSnapshot, error)
	CreateVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error
	RestoreVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error
	DeleteVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error

	// ExportInstance streams a portable backup tarball of the instance to w.
	ExportInstance(ctx context.Context, name string, w io.Writer) error
	// ImportInstance creates an instance named name from a backup tarball read
	// from r (as produced by ExportInstance).
	ImportInstance(ctx context.Context, name string, r io.Reader) error

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

	// ListOperations returns running and recently finished daemon tasks,
	// newest first.
	ListOperations(ctx context.Context) ([]Operation, error)

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
	// ListWarnings returns daemon warnings, newest last-seen first.
	ListWarnings(ctx context.Context) ([]Warning, error)
	DeleteWarning(ctx context.Context, uuid string) error
	// AcknowledgeWarning marks a warning as acknowledged without removing it.
	AcknowledgeWarning(ctx context.Context, uuid string) error
}
