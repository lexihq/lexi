package incus

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/lexihq/lexi/internal/backend"
	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

func (b *incusBackend) ListInstances(ctx context.Context) ([]backend.Instance, error) {
	full, err := b.project(ctx).GetInstancesFull(api.InstanceTypeAny)
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

func (b *incusBackend) GetInstance(ctx context.Context, name string) (backend.Instance, error) {
	full, _, err := b.project(ctx).GetInstanceFull(name)
	if err != nil {
		return backend.Instance{}, fmt.Errorf("get instance %q: %w", name, mapErr(err))
	}
	return toInstance(&full.Instance, full.State, len(full.Snapshots)), nil
}

func (b *incusBackend) CreateInstance(ctx context.Context, opt backend.CreateOptions) error {
	// The nic device built from opt.Network uses the "network" property, which
	// only works for managed networks (unmanaged ones need nictype+parent, a
	// shape this seam doesn't offer); reject unmanaged up front.
	if opt.Network != "" {
		n, _, err := b.project(ctx).GetNetwork(opt.Network)
		if err != nil {
			return fmt.Errorf("get network %q: %w", opt.Network, mapErr(err))
		}
		if !n.Managed {
			return fmt.Errorf("network %q is not managed: %w", opt.Network, backend.ErrInvalid)
		}
	}
	req, err := createRequest(opt)
	if err != nil {
		return err
	}
	op, err := b.project(ctx).CreateInstance(req)
	return waitOp(ctx, op, err, "create instance %q", opt.Name)
}

// RebuildInstance reinstalls a stopped instance from a catalog image, keeping
// its config, devices, and profiles. The image source mirrors create:
// fingerprint wins when set, otherwise the alias resolves on the images
// remote. The daemon rejects rebuilding a running instance.
func (b *incusBackend) RebuildInstance(ctx context.Context, name, image, fingerprint string) error {
	if image == "" && fingerprint == "" {
		return fmt.Errorf("rebuild image is required: %w", backend.ErrInvalid)
	}
	source := api.InstanceSource{
		Type:     "image",
		Server:   imagesRemote,
		Protocol: "simplestreams",
	}
	if fingerprint != "" {
		source.Fingerprint = fingerprint
	} else {
		source.Alias = image
	}
	op, err := b.project(ctx).RebuildInstance(name, api.InstanceRebuildPost{Source: source})
	return waitOp(ctx, op, err, "rebuild instance %q", name)
}

func (b *incusBackend) StartInstance(ctx context.Context, name string) error {
	return b.changeState(ctx, name, "start", false)
}

func (b *incusBackend) StopInstance(ctx context.Context, name string) error {
	return b.changeState(ctx, name, "stop", true)
}

func (b *incusBackend) RestartInstance(ctx context.Context, name string) error {
	return b.changeState(ctx, name, "restart", false)
}

func (b *incusBackend) PauseInstance(ctx context.Context, name string) error {
	return b.changeState(ctx, name, "freeze", false)
}

func (b *incusBackend) ResumeInstance(ctx context.Context, name string) error {
	return b.changeState(ctx, name, "unfreeze", false)
}

func (b *incusBackend) changeState(ctx context.Context, name, action string, force bool) error {
	op, err := b.project(ctx).UpdateInstanceState(name, api.InstanceStatePut{
		Action:  action,
		Timeout: -1,
		Force:   force,
	}, "")
	return waitOp(ctx, op, err, "%s instance %q", action, name)
}

func (b *incusBackend) DeleteInstance(ctx context.Context, name string) error {
	state, _, err := b.project(ctx).GetInstanceState(name)
	if err != nil {
		return fmt.Errorf("get state of %q: %w", name, mapErr(err))
	}
	if state.Status != string(backend.StatusStopped) {
		if err := b.changeState(ctx, name, "stop", true); err != nil {
			return err
		}
	}
	op, err := b.project(ctx).DeleteInstance(name)
	if err := waitOp(ctx, op, err, "delete instance %q", name); err != nil {
		return err
	}
	b.clearCPUSample(cpuSampleKey(ctx, name))
	return nil
}

func (b *incusBackend) CloneInstance(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// The copy source must be the scoped client too: the daemon resolves the
	// source from its project, not the target's.
	srv := b.project(ctx)
	source, _, err := srv.GetInstance(src)
	if err != nil {
		return fmt.Errorf("get source instance %q: %w", src, mapErr(err))
	}
	op, err := srv.CopyInstance(srv, *source, &incusclient.InstanceCopyArgs{Name: dst})
	if err != nil {
		return fmt.Errorf("clone %q to %q: %w", src, dst, mapErr(err))
	}
	if err := waitRemoteOperation(ctx, op); err != nil {
		return fmt.Errorf("clone %q to %q: %w", src, dst, mapErr(err))
	}
	return nil
}

// waitOp finishes an Incus operation call: it maps the immediate error, then
// waits with ctx, attributing both to the same label (so the wording can't drift
// between the two paths). Pass the (op, err) pair straight from the client call.
func waitOp(ctx context.Context, op incusclient.Operation, err error, format string, args ...any) error {
	if err != nil {
		return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), mapErr(err))
	}
	return nil
}

// mutateInstance applies a GET-then-PUT update: it fetches the instance, lets
// mutate adjust the writable copy, then PUTs and waits. The fetch error is
// labelled "get instance"; the update/wait error uses format/args.
func (b *incusBackend) mutateInstance(ctx context.Context, name string, mutate func(*api.InstancePut), format string, args ...any) error {
	inst, etag, err := b.project(ctx).GetInstance(name)
	if err != nil {
		return fmt.Errorf("get instance %q: %w", name, mapErr(err))
	}
	put := inst.Writable()
	mutate(&put)
	op, err := b.project(ctx).UpdateInstance(name, put, etag)
	return waitOp(ctx, op, err, format, args...)
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
			return errors.Join(ctx.Err(), fmt.Errorf("cancel remote operation: %w", err))
		}
		return ctx.Err()
	}
}

func toInstance(in *api.Instance, state *api.InstanceState, snapshots int) backend.Instance {
	return backend.Instance{
		Name:         in.Name,
		Status:       backend.InstanceStatus(in.Status),
		Image:        in.ExpandedConfig["image.description"],
		IPv4:         ipv4Addresses(state),
		Snapshots:    snapshots,
		CreatedAt:    in.CreatedAt,
		LimitsCPU:    in.ExpandedConfig["limits.cpu"],
		LimitsMemory: in.ExpandedConfig["limits.memory"],
		Profiles:     append([]string(nil), in.Profiles...),
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

func createRequest(opt backend.CreateOptions) (api.InstancesPost, error) {
	instanceType := api.InstanceTypeContainer
	switch opt.Type {
	case "", backend.TypeContainer:
	case backend.TypeVirtualMachine:
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

	req := api.InstancesPost{
		Name:   opt.Name,
		Type:   instanceType,
		Start:  opt.Start,
		Source: source,
	}
	// InstancesPost has no creation-time pool/network fields: like the incus
	// CLI's -s/-n flags, the pool rides a local "root" disk device and the
	// network a local "eth0" nic device, both shadowing any profile-supplied
	// device of the same name. The daemon validates all references.
	//
	// A zero-length profile list stays unset: the daemon applies the default
	// profile only when Profiles is nil — an explicit [] means "no profiles".
	if len(opt.Profiles) > 0 {
		req.Profiles = opt.Profiles
	}
	req.Config = opt.Config
	if opt.Pool != "" || opt.Network != "" {
		req.Devices = map[string]map[string]string{}
		if opt.Pool != "" {
			req.Devices["root"] = map[string]string{"type": "disk", "path": "/", "pool": opt.Pool}
		}
		if opt.Network != "" {
			req.Devices["eth0"] = map[string]string{"type": "nic", "name": "eth0", "network": opt.Network}
		}
	}
	return req, nil
}
