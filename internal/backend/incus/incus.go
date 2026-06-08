package incus

import (
	"context"
	"fmt"
	"sort"
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

// Compile-time proof that incusBackend satisfies the Backend contract.
var _ backend.Backend = (*incusBackend)(nil)

// incusBackend implements backend.Backend over the Incus Go client.
type incusBackend struct {
	srv  incusclient.InstanceServer
	caps backend.Capabilities

	imgMu     sync.Mutex
	imgCache  []backend.Image
	imgExpiry time.Time
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
		return backend.Instance{}, fmt.Errorf("get instance %q: %w", name, mapErr(err))
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
// per (alias, architecture), pulling distro/release/variant from image properties.
func toImages(images []api.Image) []backend.Image {
	seen := make(map[string]bool)
	out := make([]backend.Image, 0, len(images))
	for i := range images {
		img := &images[i]
		for _, al := range img.Aliases {
			if al.Name == "" {
				continue
			}
			key := al.Name + "\x00" + img.Architecture
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, backend.Image{
				Alias:        al.Name,
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
		return out[i].Arch < out[j].Arch
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
		return fmt.Errorf("snapshot %q of %q: %w", snapshot, name, mapErr(err))
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
		return fmt.Errorf("get instance %q: %w", name, mapErr(err))
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
		return fmt.Errorf("delete snapshot %q of %q: %w", snapshot, name, mapErr(err))
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
		return fmt.Errorf("get source instance %q: %w", src, mapErr(err))
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

// mapErr translates an Incus client error into a backend sentinel so the HTTP
// layer can map it to a status via errors.Is, mirroring the fake backend.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "not found"):
		return fmt.Errorf("%w: %v", backend.ErrNotFound, err)
	case strings.Contains(msg, "already exists"):
		return fmt.Errorf("%w: %v", backend.ErrConflict, err)
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
