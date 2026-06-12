package incus

import (
	"context"
	"fmt"
	"log"
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

// remoteConn is one dialed remote: its connection, probed capabilities, and
// configured address. Connections and capability probes happen once at New().
type remoteConn struct {
	srv  incusclient.InstanceServer
	caps backend.Capabilities
	addr string
}

// incusBackend implements backend.Backend over the Incus Go client. It holds
// one connection per reachable CLI-config remote; srv/caps mirror the default
// remote's entry for the hot path.
type incusBackend struct {
	srv  incusclient.InstanceServer
	caps backend.Capabilities

	// remoteName names the default remote; remotes holds every reachable
	// remote, keyed by name (always including the default). Remotes that
	// fail to dial at startup are logged and excluded until restart.
	remoteName string
	remotes    map[string]*remoteConn

	imgMu     sync.Mutex
	imgCache  []backend.Image
	imgExpiry time.Time

	cpuMu      sync.Mutex
	cpuSamples map[string]cpuSample
	cpuEpoch   uint64
}

// probeCaps interrogates one daemon for its feature set. Every flag reflects
// what that server actually supports — the UI never offers an operation the
// connected daemon can't honor.
func probeCaps(srv incusclient.InstanceServer) (backend.Capabilities, error) {
	info, _, err := srv.GetServer()
	if err != nil {
		return backend.Capabilities{}, fmt.Errorf("probe incus server: %w", err)
	}
	return backend.Capabilities{
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
		NetworkForwards: srv.HasExtension("network_forward"),
		StoredBackups:   srv.HasExtension("container_backup"),
		ImageRefresh:    srv.HasExtension("image_force_refresh"),
		CertificateEdit: srv.HasExtension("certificate_update") && srv.HasExtension("certificate_project"),
		InstanceRebuild: srv.HasExtension("instances_rebuild"),
		ISOVolumes:      srv.HasExtension("custom_volume_iso"),
		Hardware:        srv.HasExtension("resources_v2"),
		VolumeBackups:   srv.HasExtension("custom_volume_backup") && srv.HasExtension("backup_override_name"),
		Projects:        srv.HasExtension("projects"),
		ProjectUsage:    srv.HasExtension("projects") && srv.HasExtension("project_usage"),
		Events:          true, // the events API is core, no extension to probe
	}, nil
}

// New connects to Incus and probes capabilities. The default remote failing
// is fatal; every other instance-server remote in the CLI config is dialed
// best-effort — one that's down at startup is logged and excluded from the
// switcher until restart.
func New() (*incusBackend, error) {
	srv, remoteName, remoteAddr, conf, err := Connect()
	if err != nil {
		return nil, fmt.Errorf("connect to incus: %w", err)
	}
	caps, err := probeCaps(srv)
	if err != nil {
		return nil, err
	}

	remotes := map[string]*remoteConn{remoteName: {srv: srv, caps: caps, addr: remoteAddr}}
	if conf != nil {
		for name, r := range conf.Remotes {
			if name == remoteName || r.Public || (r.Protocol != "" && r.Protocol != "incus") {
				continue // images servers and the already-dialed default
			}
			rsrv, err := conf.GetInstanceServer(name)
			if err != nil {
				log.Printf("remote %q unreachable, excluded until restart: %v", name, err)
				continue
			}
			rcaps, err := probeCaps(rsrv)
			if err != nil {
				log.Printf("remote %q probe failed, excluded until restart: %v", name, err)
				continue
			}
			remotes[name] = &remoteConn{srv: rsrv, caps: rcaps, addr: r.Addr}
		}
	}
	// The switcher (and migration, which needs a target) only appear when
	// there is somewhere to switch to.
	multi := len(remotes) > 1
	for _, rc := range remotes {
		rc.caps.Remotes = multi
		rc.caps.Migrate = multi
	}

	return &incusBackend{
		srv:        srv,
		caps:       remotes[remoteName].caps,
		remoteName: remoteName,
		remotes:    remotes,
		cpuSamples: make(map[string]cpuSample),
	}, nil
}

// Capabilities reports the feature flags probed at New() for the remote the
// request is scoped to, so the UI only ever offers what that daemon supports.
func (b *incusBackend) Capabilities(ctx context.Context) backend.Capabilities {
	if rc, ok := b.remotes[backend.RemoteFromContext(ctx)]; ok {
		return rc.caps
	}
	return b.caps
}

// server returns the request's remote connection, project-unscoped (server
// config, certificates, projects themselves). An unknown remote falls back
// to the default: the HTTP layer validates the selection against ListRemotes
// before any handler runs, so this only triggers on contexts that never saw
// the middleware (tests, internal calls).
func (b *incusBackend) server(ctx context.Context) incusclient.InstanceServer {
	if rc, ok := b.remotes[backend.RemoteFromContext(ctx)]; ok {
		return rc.srv
	}
	return b.srv
}

// project returns the client scoped to the request's remote and project (the
// Backend interface contract: WithRemote/WithProject tag the ctx, unset means
// the defaults). UseProject is a cheap struct copy sharing the HTTP client;
// the daemon routes shared resource kinds (per the project's features.*) to
// the default project itself, so scoping every call is safe.
func (b *incusBackend) project(ctx context.Context) incusclient.InstanceServer {
	srv := b.server(ctx)
	if name := backend.ProjectFromContext(ctx); name != "" && name != "default" {
		return srv.UseProject(name)
	}
	return srv
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
