package incus

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adam/lxcon/internal/backend"

	"github.com/gorilla/websocket"
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

// cpuSample records a cumulative CPU-time reading so the next Metrics call can
// turn the delta into a CPU percentage.
type cpuSample struct {
	nanos int64
	at    time.Time
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
			ServerInfo: fmt.Sprintf("Incus %s", info.Environment.ServerVersion),
			Snapshots:  true,
			Clone:      true,
			Backup:     true,
			Console:    true,
			Metrics:    true,
			Limits:     true,
		},
		cpuSamples: make(map[string]cpuSample),
	}, nil
}

// Capabilities reports the server info and feature flags probed at New().
func (b *incusBackend) Capabilities() backend.Capabilities { return b.caps }

// --- read paths ---

func (b *incusBackend) ListInstances(_ context.Context) ([]backend.Instance, error) {
	full, err := b.srv.GetInstancesFull(api.InstanceTypeAny)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	out := make([]backend.Instance, 0, len(full))
	for i := range full {
		out = append(out, toInstance(&full[i].Instance, full[i].State, len(full[i].Snapshots)))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (b *incusBackend) GetInstance(_ context.Context, name string) (backend.Instance, error) {
	full, _, err := b.srv.GetInstanceFull(name)
	if err != nil {
		return backend.Instance{}, fmt.Errorf("get instance %q: %w", name, mapErr(err))
	}
	return toInstance(&full.Instance, full.State, len(full.Snapshots)), nil
}

// Metrics reads a point-in-time resource snapshot from the instance state.
// Disk usage is summed across devices and network counters across every
// interface except loopback. CPUPercent reads 0 until a prior sample exists.
func (b *incusBackend) Metrics(_ context.Context, name string) (backend.Metrics, error) {
	epoch := b.cpuEpochSnapshot()
	state, _, err := b.srv.GetInstanceState(name)
	if err != nil {
		return backend.Metrics{}, fmt.Errorf("get state of %q: %w", name, mapErr(err))
	}
	m := backend.Metrics{
		MemoryUsage: state.Memory.Usage,
		MemoryTotal: state.Memory.Total,
		Processes:   state.Processes,
		CPUPercent:  b.cpuPercent(name, state.CPU.Usage, epoch),
	}
	for _, d := range state.Disk {
		m.DiskUsage += d.Usage
	}
	for iface, n := range state.Network {
		if iface == "lo" {
			continue
		}
		m.NetworkRx += n.Counters.BytesReceived
		m.NetworkTx += n.Counters.BytesSent
	}
	return m, nil
}

// cpuPercent turns the delta between two cumulative CPU-time samples into a
// percentage. It records the new sample and returns 0 on the first reading or
// any non-positive interval.
func (b *incusBackend) cpuEpochSnapshot() uint64 {
	b.cpuMu.Lock()
	defer b.cpuMu.Unlock()
	return b.cpuEpoch
}

func (b *incusBackend) cpuPercent(name string, cpuNanos int64, epoch uint64) float64 {
	now := time.Now()
	b.cpuMu.Lock()
	defer b.cpuMu.Unlock()
	for sampleName, sample := range b.cpuSamples {
		if now.Sub(sample.at) > cpuSampleTTL {
			delete(b.cpuSamples, sampleName)
		}
	}
	if epoch != b.cpuEpoch {
		return 0
	}
	prev, ok := b.cpuSamples[name]
	b.cpuSamples[name] = cpuSample{nanos: cpuNanos, at: now}
	if !ok {
		return 0
	}
	elapsed := now.Sub(prev.at).Nanoseconds()
	delta := cpuNanos - prev.nanos
	if elapsed <= 0 || delta < 0 {
		return 0
	}
	return float64(delta) / float64(elapsed) * 100
}

func (b *incusBackend) ListSnapshots(_ context.Context, name string) ([]backend.Snapshot, error) {
	snaps, err := b.srv.GetInstanceSnapshots(name)
	if err != nil {
		return nil, fmt.Errorf("list snapshots of %q: %w", name, mapErr(err))
	}
	out := make([]backend.Snapshot, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, backend.Snapshot{
			Name:      snapshotShortName(s.Name),
			CreatedAt: s.CreatedAt,
			Stateful:  s.Stateful,
		})
	}
	return out, nil
}

// ListImages returns the full simplestreams catalog (one entry per alias), served
// from a lazy, mutex-guarded cache so the search UI can filter without refetching.
func (b *incusBackend) ListImages(_ context.Context) ([]backend.Image, error) {
	b.imgMu.Lock()
	defer b.imgMu.Unlock()

	if b.imgCache != nil && time.Now().Before(b.imgExpiry) {
		return append([]backend.Image(nil), b.imgCache...), nil
	}

	is, err := incusclient.ConnectSimpleStreams(imagesRemote, nil)
	if err != nil {
		return nil, fmt.Errorf("connect images remote: %w", err)
	}
	images, err := is.GetImages()
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}

	b.imgCache = toImages(images)
	b.imgExpiry = time.Now().Add(imageCacheTTL)
	return append([]backend.Image(nil), b.imgCache...), nil
}

// toImages flattens the simplestreams catalog into one launchable domain Image
// per (alias, architecture, type), pulling filter fields from image properties.
func toImages(images []api.Image) []backend.Image {
	seen := make(map[string]bool)
	out := make([]backend.Image, 0, len(images))
	for i := range images {
		img := &images[i]
		for _, al := range img.Aliases {
			if al.Name == "" {
				continue
			}
			key := al.Name + "\x00" + img.Architecture + "\x00" + img.Type
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, backend.Image{
				Alias:        al.Name,
				Fingerprint:  img.Fingerprint,
				Description:  firstNonEmpty(al.Description, img.Properties["description"]),
				Arch:         img.Architecture,
				SizeBytes:    img.Size,
				Distribution: strings.ToLower(firstNonEmpty(img.Properties["os"], distroFromAlias(al.Name))),
				Release:      img.Properties["release"],
				Variant:      img.Properties["variant"],
				Type:         img.Type,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Alias != out[j].Alias {
			return out[i].Alias < out[j].Alias
		}
		if out[i].Arch != out[j].Arch {
			return out[i].Arch < out[j].Arch
		}
		return out[i].Type < out[j].Type
	})
	return out
}

// distroFromAlias falls back to the first path segment of an alias (e.g.
// "debian" from "debian/12") when the image carries no os property.
func distroFromAlias(alias string) string {
	distro, _, _ := strings.Cut(alias, "/")
	return distro
}

// --- write paths ---

func (b *incusBackend) CreateInstance(ctx context.Context, opt backend.CreateOptions) error {
	req, err := createRequest(opt)
	if err != nil {
		return err
	}
	op, err := b.srv.CreateInstance(req)
	if err != nil {
		return fmt.Errorf("create instance %q: %w", opt.Name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("create instance %q: %w", opt.Name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) StartInstance(ctx context.Context, name string) error {
	return b.changeState(ctx, name, "start", false)
}

func (b *incusBackend) StopInstance(ctx context.Context, name string) error {
	return b.changeState(ctx, name, "stop", true)
}

func (b *incusBackend) changeState(ctx context.Context, name, action string, force bool) error {
	op, err := b.srv.UpdateInstanceState(name, api.InstanceStatePut{
		Action:  action,
		Timeout: -1,
		Force:   force,
	}, "")
	if err != nil {
		return fmt.Errorf("%s instance %q: %w", action, name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("%s instance %q: %w", action, name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) DeleteInstance(ctx context.Context, name string) error {
	state, _, err := b.srv.GetInstanceState(name)
	if err != nil {
		return fmt.Errorf("get state of %q: %w", name, mapErr(err))
	}
	if state.Status != "Stopped" {
		if err := b.changeState(ctx, name, "stop", true); err != nil {
			return err
		}
	}
	op, err := b.srv.DeleteInstance(name)
	if err != nil {
		return fmt.Errorf("delete instance %q: %w", name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("delete instance %q: %w", name, mapErr(err))
	}
	b.clearCPUSample(name)
	return nil
}

func (b *incusBackend) clearCPUSample(name string) {
	b.cpuMu.Lock()
	defer b.cpuMu.Unlock()
	delete(b.cpuSamples, name)
	b.cpuEpoch++
}

// --- snapshot & clone ---

func (b *incusBackend) CreateSnapshot(ctx context.Context, name, snapshot string) error {
	op, err := b.srv.CreateInstanceSnapshot(name, api.InstanceSnapshotsPost{Name: snapshot})
	if err != nil {
		return fmt.Errorf("snapshot %q of %q: %w", snapshot, name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("snapshot %q of %q: %w", snapshot, name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) RestoreSnapshot(ctx context.Context, name, snapshot string) error {
	// GET-then-PUT preserves the instance config; Restore triggers the rollback.
	inst, etag, err := b.srv.GetInstance(name)
	if err != nil {
		return fmt.Errorf("get instance %q: %w", name, mapErr(err))
	}
	put := inst.Writable()
	put.Restore = snapshot
	op, err := b.srv.UpdateInstance(name, put, etag)
	if err != nil {
		return fmt.Errorf("restore %q on %q: %w", snapshot, name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("restore %q on %q: %w", snapshot, name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) DeleteSnapshot(ctx context.Context, name, snapshot string) error {
	op, err := b.srv.DeleteInstanceSnapshot(name, snapshot)
	if err != nil {
		return fmt.Errorf("delete snapshot %q of %q: %w", snapshot, name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("delete snapshot %q of %q: %w", snapshot, name, mapErr(err))
	}
	return nil
}

// UpdateLimits sets or clears limits.cpu/limits.memory on the instance's local
// config (GET-then-PUT, matching RestoreSnapshot). Empty values delete the key.
func (b *incusBackend) UpdateLimits(ctx context.Context, name string, l backend.Limits) error {
	inst, etag, err := b.srv.GetInstance(name)
	if err != nil {
		return fmt.Errorf("get instance %q: %w", name, mapErr(err))
	}
	put := inst.Writable()
	if put.Config == nil {
		put.Config = map[string]string{}
	}
	setOrDelete(put.Config, "limits.cpu", l.CPU)
	setOrDelete(put.Config, "limits.memory", l.Memory)

	op, err := b.srv.UpdateInstance(name, put, etag)
	if err != nil {
		return fmt.Errorf("update limits on %q: %w", name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("update limits on %q: %w", name, mapErr(err))
	}
	return nil
}

// ExportInstance creates a backup, spools it to a temp file (the client API
// needs an io.WriteSeeker), then streams the spooled file to w. The server-side
// backup is removed via a deferred best-effort cleanup so it is deleted on every
// path, including errors between creation and streaming. The backup name is
// timestamped to avoid colliding with concurrent runs.
func (b *incusBackend) ExportInstance(ctx context.Context, name string, w io.Writer) error {
	backupName := fmt.Sprintf("lxcon-export-%d", time.Now().UnixNano())

	op, err := b.srv.CreateInstanceBackup(name, api.InstanceBackupsPost{
		Name:                 backupName,
		CompressionAlgorithm: "gzip",
	})
	if err != nil {
		return fmt.Errorf("create backup of %q: %w", name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("create backup of %q: %w", name, mapErr(err))
	}
	defer b.deleteBackup(name, backupName)

	tmp, err := os.CreateTemp("", "lxcon-export-*.tar.gz")
	if err != nil {
		return fmt.Errorf("spool backup of %q: %w", name, err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	if _, err := b.srv.GetInstanceBackupFile(name, backupName, &incusclient.BackupFileRequest{BackupFile: tmp}); err != nil {
		return fmt.Errorf("download backup of %q: %w", name, mapErr(err))
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind backup of %q: %w", name, err)
	}
	if _, err := io.Copy(w, tmp); err != nil {
		return fmt.Errorf("stream backup of %q: %w", name, err)
	}
	return nil
}

// ConsoleLog reads the instance's console log buffer into a string.
func (b *incusBackend) ConsoleLog(_ context.Context, name string) (string, error) {
	rc, err := b.srv.GetInstanceConsoleLog(name, &incusclient.InstanceConsoleLogArgs{})
	if err != nil {
		return "", fmt.Errorf("get console log of %q: %w", name, mapErr(err))
	}
	defer rc.Close()

	content, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("read console log of %q: %w", name, err)
	}
	return string(content), nil
}

// Exec runs an interactive command (defaulting to /bin/sh, which the curated
// images all provide) bridging req.Stdin/Stdout to a single PTY. Window resizes
// from req.Resize are forwarded over the exec control socket until the session
// ends.
func (b *incusBackend) Exec(ctx context.Context, name string, req backend.ExecRequest) error {
	command := req.Command
	if len(command) == 0 {
		command = []string{"/bin/sh"}
	}

	dataDone := make(chan bool)
	op, err := b.srv.ExecInstance(name, api.InstanceExecPost{
		Command:     command,
		WaitForWS:   true,
		Interactive: true,
		Width:       req.Width,
		Height:      req.Height,
	}, &incusclient.InstanceExecArgs{
		Stdin:    req.Stdin,
		Stdout:   req.Stdout,
		Control:  execControl(ctx, req.Resize),
		DataDone: dataDone,
	})
	if err != nil {
		return fmt.Errorf("exec on %q: %w", name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("exec on %q: %w", name, mapErr(err))
	}

	// Wait for the I/O streams to flush before returning so the caller can close
	// its side cleanly; honor cancellation so a dropped client never wedges here.
	select {
	case <-dataDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// execControl returns a control-socket handler that forwards window resizes from
// resize as exec control messages. It returns nil when resize is nil so the
// client skips control handling entirely.
func execControl(ctx context.Context, resize <-chan backend.WinSize) func(*websocket.Conn) {
	if resize == nil {
		return nil
	}
	return func(conn *websocket.Conn) {
		for {
			select {
			case <-ctx.Done():
				return
			case size, ok := <-resize:
				if !ok {
					return
				}
				if err := conn.WriteJSON(resizeControl(size)); err != nil {
					return
				}
			}
		}
	}
}

// resizeControl builds the exec control message for a window resize.
func resizeControl(size backend.WinSize) api.InstanceExecControl {
	return api.InstanceExecControl{
		Command: "window-resize",
		Args: map[string]string{
			"width":  strconv.Itoa(size.Cols),
			"height": strconv.Itoa(size.Rows),
		},
	}
}

// ImportInstance creates an instance named name from a backup tarball streamed
// from r (as produced by ExportInstance).
func (b *incusBackend) ImportInstance(ctx context.Context, name string, r io.Reader) error {
	op, err := b.srv.CreateInstanceFromBackup(incusclient.InstanceBackupArgs{
		BackupFile: r,
		Name:       name,
	})
	if err != nil {
		return fmt.Errorf("import instance %q: %w", name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("import instance %q: %w", name, mapErr(err))
	}
	return nil
}

// deleteBackup removes the temporary server-side backup created during export.
// It is best-effort cleanup invoked via defer with its own bounded context,
// detached from the request: a client disconnecting as the download finishes
// must not abort cleanup and leak the backup. A failure can no longer change the
// already-streamed result, so it is dropped (leaks surface via integration
// cleanup).
func (b *incusBackend) deleteBackup(name, backupName string) {
	ctx, cancel := context.WithTimeout(context.Background(), backupDeleteTimeout)
	defer cancel()

	op, err := b.srv.DeleteInstanceBackup(name, backupName)
	if err != nil {
		return
	}
	_ = op.WaitContext(ctx)
}

func setOrDelete(m map[string]string, key, val string) {
	if val == "" {
		delete(m, key)
		return
	}
	m[key] = val
}

func (b *incusBackend) CloneInstance(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, _, err := b.srv.GetInstance(src)
	if err != nil {
		return fmt.Errorf("get source instance %q: %w", src, mapErr(err))
	}
	op, err := b.srv.CopyInstance(b.srv, *source, &incusclient.InstanceCopyArgs{Name: dst})
	if err != nil {
		return fmt.Errorf("clone %q to %q: %w", src, dst, mapErr(err))
	}
	if err := waitRemoteOperation(ctx, op); err != nil {
		return fmt.Errorf("clone %q to %q: %w", src, dst, mapErr(err))
	}
	return nil
}

func waitRemoteOperation(ctx context.Context, op incusclient.RemoteOperation) error {
	done := make(chan error, 1)
	go func() {
		done <- op.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if err := op.CancelTarget(); err != nil {
			return fmt.Errorf("%w: cancel remote operation: %v", ctx.Err(), err)
		}
		return ctx.Err()
	}
}

// --- mappers / helpers ---

func toInstance(in *api.Instance, state *api.InstanceState, snapshots int) backend.Instance {
	return backend.Instance{
		Name:         in.Name,
		Status:       in.Status,
		Image:        in.ExpandedConfig["image.description"],
		IPv4:         ipv4Addresses(state),
		Snapshots:    snapshots,
		CreatedAt:    in.CreatedAt,
		LimitsCPU:    in.ExpandedConfig["limits.cpu"],
		LimitsMemory: in.ExpandedConfig["limits.memory"],
	}
}

// ipv4Addresses extracts global IPv4 addresses across the instance's non-loopback
// interfaces.
func ipv4Addresses(state *api.InstanceState) []string {
	if state == nil {
		return nil
	}
	var out []string
	for iface, net := range state.Network {
		if iface == "lo" {
			continue
		}
		for _, a := range net.Addresses {
			if a.Family == "inet" && a.Scope == "global" {
				out = append(out, a.Address)
			}
		}
	}
	sort.Strings(out)
	return out
}

func snapshotShortName(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

func createRequest(opt backend.CreateOptions) (api.InstancesPost, error) {
	instanceType := api.InstanceTypeContainer
	switch opt.Type {
	case "", string(api.InstanceTypeContainer):
	case string(api.InstanceTypeVM):
		instanceType = api.InstanceTypeVM
	default:
		return api.InstancesPost{}, fmt.Errorf("image type %q: %w", opt.Type, backend.ErrUnsupported)
	}

	source := api.InstanceSource{
		Type:     "image",
		Server:   imagesRemote,
		Protocol: "simplestreams",
	}
	if opt.Fingerprint != "" {
		source.Fingerprint = opt.Fingerprint
	} else {
		source.Alias = opt.Image
	}

	return api.InstancesPost{
		Name:   opt.Name,
		Type:   instanceType,
		Start:  opt.Start,
		Source: source,
	}, nil
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
	case api.StatusErrorCheck(err, http.StatusConflict):
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
	case strings.Contains(msg, "already exists"):
		return fmt.Errorf("%w: %w", backend.ErrConflict, err)
	case strings.Contains(msg, "bad request"),
		strings.Contains(msg, "invalid value"),
		strings.Contains(msg, "invalid config"):
		return fmt.Errorf("%w: %w", backend.ErrInvalid, err)
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
