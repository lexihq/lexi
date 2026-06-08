// Package backend defines the driver-agnostic contract lxcon drives the UI
// through. Domain types are intentionally decoupled from incus/shared/api so a
// future liblxc driver can implement the same interface without a rewrite.
package backend

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors drivers wrap (with %w) so the HTTP layer can map them to
// status codes via errors.Is, independent of any driver's wording.
var (
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("already exists")
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
}

// Instance is a system container.
type Instance struct {
	Name      string
	Status    string // Running | Stopped | ...
	Image     string // base image description, if known
	IPv4      []string
	Snapshots int
	CreatedAt time.Time
}

// Snapshot is a point-in-time snapshot of an instance.
type Snapshot struct {
	Name      string
	CreatedAt time.Time
	Stateful  bool
}

// Image is an entry in the create-from-image dropdown.
type Image struct {
	Alias       string // e.g. "debian/12"
	Description string
	Arch        string
	SizeBytes   int64
}

// CreateOptions parameterizes CreateInstance.
type CreateOptions struct {
	Name  string
	Image string // alias on the images remote
	Start bool
}

// Backend is the single seam between the HTTP layer and a container driver.
type Backend interface {
	Capabilities() Capabilities

	ListInstances(ctx context.Context) ([]Instance, error)
	GetInstance(ctx context.Context, name string) (Instance, error)
	CreateInstance(ctx context.Context, opt CreateOptions) error
	StartInstance(ctx context.Context, name string) error
	StopInstance(ctx context.Context, name string) error
	DeleteInstance(ctx context.Context, name string) error // stop-then-delete

	ListSnapshots(ctx context.Context, name string) ([]Snapshot, error)
	CreateSnapshot(ctx context.Context, name, snapshot string) error
	RestoreSnapshot(ctx context.Context, name, snapshot string) error
	DeleteSnapshot(ctx context.Context, name, snapshot string) error

	CloneInstance(ctx context.Context, src, dst string) error

	ListImages(ctx context.Context) ([]Image, error) // for the create dropdown
}
