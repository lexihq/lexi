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
}

// InstanceConfig is an instance's editable local config plus its devices. Config
// excludes volatile.* and limits.cpu/limits.memory, which are managed elsewhere
// and preserved on update. Devices is the full expanded set (read-only);
// LocalDevices is the instance-owned subset (editable).
type InstanceConfig struct {
	Config       map[string]string
	Devices      map[string]map[string]string
	LocalDevices map[string]map[string]string
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
	CreateSnapshot(ctx context.Context, name, snapshot string) error
	RestoreSnapshot(ctx context.Context, name, snapshot string) error
	DeleteSnapshot(ctx context.Context, name, snapshot string) error

	CloneInstance(ctx context.Context, src, dst string) error

	UpdateLimits(ctx context.Context, name string, l Limits) error
	Metrics(ctx context.Context, name string) (Metrics, error)

	ListProfiles(ctx context.Context) ([]Profile, error)
	GetProfile(ctx context.Context, name string) (Profile, error)
	// SetInstanceProfiles replaces the instance's profile list (ordered; later
	// profiles override earlier ones).
	SetInstanceProfiles(ctx context.Context, name string, profiles []string) error

	GetInstanceConfig(ctx context.Context, name string) (InstanceConfig, error)
	UpdateInstanceConfig(ctx context.Context, name string, config map[string]string) error
	// AddDevice attaches (or overwrites) a local device on the instance.
	AddDevice(ctx context.Context, name, device string, config map[string]string) error
	// RemoveDevice detaches a local device. The device must exist (ErrNotFound).
	RemoveDevice(ctx context.Context, name, device string) error

	ListNetworks(ctx context.Context) ([]Network, error)
	GetNetwork(ctx context.Context, name string) (Network, error)
	CreateNetwork(ctx context.Context, n Network) error
	DeleteNetwork(ctx context.Context, name string) error

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
}
