package incus

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/adam/lxcon/internal/backend"

	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

// imagesRemote is the public image server lxcon pulls base images from in v1.
const imagesRemote = "https://images.linuxcontainers.org"

// curatedAliases is the v1 create-from-image set. A full image browser is post-v1.
var curatedAliases = []string{"debian/12", "ubuntu/24.04", "alpine/edge"}

// Compile-time proof that incusBackend satisfies the Backend contract.
var _ backend.Backend = (*incusBackend)(nil)

// incusBackend implements backend.Backend over the Incus Go client.
type incusBackend struct {
	srv  incusclient.InstanceServer
	caps backend.Capabilities
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
		},
	}, nil
}

// Capabilities reports the server info and feature flags probed at New().
func (b *incusBackend) Capabilities() backend.Capabilities { return b.caps }

// --- read paths ---

func (b *incusBackend) ListInstances(_ context.Context) ([]backend.Instance, error) {
	full, err := b.srv.GetInstancesFull(api.InstanceTypeContainer)
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
		return backend.Instance{}, fmt.Errorf("get instance %q: %w", name, err)
	}
	return toInstance(&full.Instance, full.State, len(full.Snapshots)), nil
}

func (b *incusBackend) ListSnapshots(_ context.Context, name string) ([]backend.Snapshot, error) {
	snaps, err := b.srv.GetInstanceSnapshots(name)
	if err != nil {
		return nil, fmt.Errorf("list snapshots of %q: %w", name, err)
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

func (b *incusBackend) ListImages(_ context.Context) ([]backend.Image, error) {
	is, err := incusclient.ConnectSimpleStreams(imagesRemote, nil)
	if err != nil {
		return nil, fmt.Errorf("connect images remote: %w", err)
	}
	images, err := is.GetImages()
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}

	wantArch := hostArch()
	curated := make(map[string]bool, len(curatedAliases))
	for _, a := range curatedAliases {
		curated[a] = true
	}

	// Dedupe by alias, preferring an image built for the host architecture.
	chosen := make(map[string]backend.Image)
	for _, img := range images {
		for _, al := range img.Aliases {
			if !curated[al.Name] {
				continue
			}
			cand := backend.Image{
				Alias:       al.Name,
				Description: firstNonEmpty(al.Description, img.Properties["description"]),
				Arch:        img.Architecture,
				SizeBytes:   img.Size,
			}
			if cur, ok := chosen[al.Name]; !ok || (cur.Arch != wantArch && cand.Arch == wantArch) {
				chosen[al.Name] = cand
			}
		}
	}

	out := make([]backend.Image, 0, len(curatedAliases))
	for _, a := range curatedAliases {
		if img, ok := chosen[a]; ok {
			out = append(out, img)
		}
	}
	return out, nil
}

// --- write paths ---

func (b *incusBackend) CreateInstance(ctx context.Context, opt backend.CreateOptions) error {
	op, err := b.srv.CreateInstance(api.InstancesPost{
		Name:  opt.Name,
		Type:  api.InstanceTypeContainer,
		Start: opt.Start,
		Source: api.InstanceSource{
			Type:     "image",
			Server:   imagesRemote,
			Protocol: "simplestreams",
			Alias:    opt.Image,
		},
	})
	if err != nil {
		return fmt.Errorf("create instance %q: %w", opt.Name, err)
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("create instance %q: %w", opt.Name, err)
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
		return fmt.Errorf("%s instance %q: %w", action, name, err)
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("%s instance %q: %w", action, name, err)
	}
	return nil
}

func (b *incusBackend) DeleteInstance(ctx context.Context, name string) error {
	state, _, err := b.srv.GetInstanceState(name)
	if err != nil {
		return fmt.Errorf("get state of %q: %w", name, err)
	}
	if state.Status != "Stopped" {
		if err := b.changeState(ctx, name, "stop", true); err != nil {
			return err
		}
	}
	op, err := b.srv.DeleteInstance(name)
	if err != nil {
		return fmt.Errorf("delete instance %q: %w", name, err)
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("delete instance %q: %w", name, err)
	}
	return nil
}

// --- snapshot & clone ---

func (b *incusBackend) CreateSnapshot(ctx context.Context, name, snapshot string) error {
	op, err := b.srv.CreateInstanceSnapshot(name, api.InstanceSnapshotsPost{Name: snapshot})
	if err != nil {
		return fmt.Errorf("snapshot %q of %q: %w", snapshot, name, err)
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("snapshot %q of %q: %w", snapshot, name, err)
	}
	return nil
}

func (b *incusBackend) RestoreSnapshot(ctx context.Context, name, snapshot string) error {
	// GET-then-PUT preserves the instance config; Restore triggers the rollback.
	inst, etag, err := b.srv.GetInstance(name)
	if err != nil {
		return fmt.Errorf("get instance %q: %w", name, err)
	}
	put := inst.Writable()
	put.Restore = snapshot
	op, err := b.srv.UpdateInstance(name, put, etag)
	if err != nil {
		return fmt.Errorf("restore %q on %q: %w", snapshot, name, err)
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("restore %q on %q: %w", snapshot, name, err)
	}
	return nil
}

func (b *incusBackend) DeleteSnapshot(ctx context.Context, name, snapshot string) error {
	op, err := b.srv.DeleteInstanceSnapshot(name, snapshot)
	if err != nil {
		return fmt.Errorf("delete snapshot %q of %q: %w", snapshot, name, err)
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("delete snapshot %q of %q: %w", snapshot, name, err)
	}
	return nil
}

func (b *incusBackend) CloneInstance(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, _, err := b.srv.GetInstance(src)
	if err != nil {
		return fmt.Errorf("get source instance %q: %w", src, err)
	}
	op, err := b.srv.CopyInstance(b.srv, *source, &incusclient.InstanceCopyArgs{Name: dst})
	if err != nil {
		return fmt.Errorf("clone %q to %q: %w", src, dst, err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("clone %q to %q: %w", src, dst, err)
	}
	return nil
}

// --- mappers / helpers ---

func toInstance(in *api.Instance, state *api.InstanceState, snapshots int) backend.Instance {
	return backend.Instance{
		Name:      in.Name,
		Status:    in.Status,
		Image:     in.ExpandedConfig["image.description"],
		IPv4:      ipv4Addresses(state),
		Snapshots: snapshots,
		CreatedAt: in.CreatedAt,
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

// hostArch maps Go's GOARCH onto the incus/simplestreams architecture name.
func hostArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return runtime.GOARCH
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
