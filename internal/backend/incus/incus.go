package incus

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/adam/lxcon/internal/backend"
	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

// imagesRemote is the public image server lxcon pulls base images from.
const imagesRemote = "https://images.linuxcontainers.org"

// imageCacheTTL bounds how long the full simplestreams catalog is reused before
// a refetch, so per-keystroke filtering never hits the network.
const imageCacheTTL = time.Hour

// cpuSampleTTL bounds stale metric state for instances deleted outside lxcon or
// requests that finish racing with deletion.
const cpuSampleTTL = 10 * time.Minute

// backupDeleteTimeout bounds the detached cleanup of a temporary export backup.
const backupDeleteTimeout = 30 * time.Second

// Compile-time proof that incusBackend satisfies the Backend contract.
var _ backend.Backend = (*incusBackend)(nil)

// incusBackend implements backend.Backend over the Incus Go client.
type incusBackend struct {
	srv  incusclient.InstanceServer
	caps backend.Capabilities

	imgMu     sync.Mutex
	imgCache  []backend.Image
	imgExpiry time.Time

	cpuMu      sync.Mutex
	cpuSamples map[string]cpuSample
	cpuEpoch   uint64
}

// New connects to Incus (default remote) and probes the server to populate
// capabilities. It returns a clear error if the daemon is unreachable.
func New() (*incusBackend, error) {
	srv, err := Connect()
	if err != nil {
		return nil, fmt.Errorf("connect to incus: %w", err)
	}
	info, _, err := srv.GetServer()
	if err != nil {
		return nil, fmt.Errorf("probe incus server: %w", err)
	}
	return &incusBackend{
		srv: srv,
		caps: backend.Capabilities{
			Tier:       backend.TierIncus,
			ServerInfo: "Incus " + info.Environment.ServerVersion,
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
			FileDelete:      srv.HasExtension("file_delete"),
			FileMkdir:       srv.HasExtension("directory_manipulation"),
			ServerAdmin:     true,
			NetworkACLs:     srv.HasExtension("network_acl"),
			VolumeBackups:   srv.HasExtension("custom_volume_backup") && srv.HasExtension("backup_override_name"),
			Projects:        srv.HasExtension("projects"),
			Events:          true, // the events API is core, no extension to probe
		},
		cpuSamples: make(map[string]cpuSample),
	}, nil
}

// Capabilities reports the server info and feature flags probed at New().
func (b *incusBackend) Capabilities(_ context.Context) backend.Capabilities { return b.caps }

// project returns the client scoped to the request's project (the Backend
// interface contract: WithProject tags the ctx, unset means default), or the
// bare client. UseProject is a cheap struct copy sharing the HTTP client;
// the daemon routes shared resource kinds (per the project's features.*) to
// the default project itself, so scoping every call is safe.
func (b *incusBackend) project(ctx context.Context) incusclient.InstanceServer {
	if name := backend.ProjectFromContext(ctx); name != "" && name != "default" {
		return b.srv.UseProject(name)
	}
	return b.srv
}

// mapErr translates an Incus client error into a backend sentinel so the HTTP
// layer can map it to a status via errors.Is, mirroring the fake backend.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case api.StatusErrorCheck(err, http.StatusNotFound):
		return fmt.Errorf("%w: %w", backend.ErrNotFound, err)
	case api.StatusErrorCheck(err, http.StatusConflict),
		api.StatusErrorCheck(err, http.StatusPreconditionFailed): // etag race
		return fmt.Errorf("%w: %w", backend.ErrConflict, err)
	case api.StatusErrorCheck(err, http.StatusBadRequest):
		return fmt.Errorf("%w: %w", backend.ErrInvalid, err)
	}

	// Operation wait errors can arrive as plain text after the client has
	// flattened the operation's error field.
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "not found"):
		return fmt.Errorf("%w: %w", backend.ErrNotFound, err)
	case strings.Contains(msg, "already exists"),
		strings.Contains(msg, "precondition failed"),
		// The daemon skips an existence pre-check on some create paths
		// (projects, raced volume imports) and surfaces the database
		// constraint directly.
		strings.Contains(msg, "unique constraint failed"):
		return fmt.Errorf("%w: %w", backend.ErrConflict, err)
	case strings.Contains(msg, "bad request"),
		strings.Contains(msg, "invalid value"),
		strings.Contains(msg, "invalid config"):
		return fmt.Errorf("%w: %w", backend.ErrInvalid, err)
	case strings.Contains(msg, "missing the required") && strings.Contains(msg, "api extension"):
		// The client pre-checks extensions ("The server is missing the
		// required %q API extension") for verbs the daemon doesn't support.
		return fmt.Errorf("%w: %w", backend.ErrUnsupported, err)
	}
	return err
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
