package fake

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/lexihq/lexi/internal/backend"
)

func (f *Fake) ListInstances(ctx context.Context) ([]backend.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	out := make([]backend.Instance, 0, len(sp.instances))
	for _, in := range sp.instances {
		out = append(out, f.view(f.featureSpace(ctx, "features.profiles"), in))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetInstance(ctx context.Context, name string) (backend.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return backend.Instance{}, notFound(name)
	}
	return f.view(f.featureSpace(ctx, "features.profiles"), in), nil
}

func (f *Fake) CreateInstance(ctx context.Context, opt backend.CreateOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	// Same name rule as the daemon: fake-backed tests must reject the names
	// production does. No apiNameEnds here — that tail rule applies to
	// projects/volumes, and single-character instance names are legal.
	if !validAPIName(opt.Name) {
		return invalid("invalid instance name %q", opt.Name)
	}
	if _, ok := sp.instances[opt.Name]; ok {
		return conflict("instance %q already exists", opt.Name)
	}
	// Validate references up front so a failed create leaves nothing behind.
	for _, p := range opt.Profiles {
		if _, ok := sp.profiles[p]; !ok {
			return invalid("unknown profile %q", p)
		}
	}
	if opt.Pool != "" {
		if _, ok := f.remote(ctx).pools[opt.Pool]; !ok {
			return notFoundf("storage pool %q", opt.Pool)
		}
	}
	if opt.Network != "" {
		n, ok := f.networkSpace(ctx).networks[opt.Network]
		if !ok {
			return notFoundf("network %q", opt.Network)
		}
		if !n.Managed {
			return invalid("network %q is not managed", opt.Network)
		}
	}
	status := backend.StatusStopped
	if opt.Start {
		status = backend.StatusRunning
	}
	profiles := opt.Profiles
	if len(profiles) == 0 {
		profiles = []string{"default"}
	}
	// limits.cpu/limits.memory live on the Instance view (the real driver
	// derives them from expanded config); everything else is instance config.
	config := map[string]string{}
	var limitsCPU, limitsMemory string
	for k, v := range opt.Config {
		switch k {
		case "limits.cpu":
			limitsCPU = v
		case "limits.memory":
			limitsMemory = v
		default:
			config[k] = v
		}
	}
	devices := map[string]map[string]string{}
	if opt.Pool != "" {
		devices["root"] = map[string]string{"type": "disk", "path": "/", "pool": opt.Pool}
	}
	if opt.Network != "" {
		devices["eth0"] = map[string]string{"type": "nic", "name": "eth0", "network": opt.Network}
	}
	sp.instances[opt.Name] = &instance{
		Instance: backend.Instance{
			Name:         opt.Name,
			Status:       status,
			Image:        opt.Image,
			CreatedAt:    f.now(),
			Profiles:     append([]string(nil), profiles...),
			LimitsCPU:    limitsCPU,
			LimitsMemory: limitsMemory,
		},
		config:  config,
		devices: devices,
		files:   seedFiles(opt.Name),
	}
	f.logOp(sp, fmt.Sprintf("Creating instance %q", opt.Name))
	return nil
}

func (f *Fake) RebuildInstance(ctx context.Context, name, image, fingerprint string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	if image == "" && fingerprint == "" {
		return invalid("rebuild image is required")
	}
	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	if in.Status != backend.StatusStopped {
		return invalid("instance %q must be stopped to rebuild", name)
	}
	in.Image = image
	f.logOp(sp, fmt.Sprintf("Rebuilding instance %q", name))
	return nil
}

func (f *Fake) StartInstance(ctx context.Context, name string) error {
	return f.setStatus(ctx, name, backend.StatusRunning, "Starting")
}

func (f *Fake) StopInstance(ctx context.Context, name string) error {
	return f.setStatus(ctx, name, backend.StatusStopped, "Stopping")
}

func (f *Fake) RestartInstance(ctx context.Context, name string) error {
	return f.setStatus(ctx, name, backend.StatusRunning, "Restarting")
}

func (f *Fake) PauseInstance(ctx context.Context, name string) error {
	return f.setStatus(ctx, name, backend.StatusFrozen, "Pausing")
}

func (f *Fake) ResumeInstance(ctx context.Context, name string) error {
	return f.setStatus(ctx, name, backend.StatusRunning, "Resuming")
}

func (f *Fake) setStatus(ctx context.Context, name string, status backend.InstanceStatus, verb string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return notFound(name)
	}
	in.Status = status
	// DHCP parity: a started instance gets an address (stable per start), a
	// stopped one releases it. The network leases view derives from this.
	switch status {
	case backend.StatusRunning:
		if len(in.IPv4) == 0 {
			sp.ipSeq++
			in.IPv4 = []string{fmt.Sprintf("10.0.3.%d", 20+sp.ipSeq%200)}
		}
	case backend.StatusStopped:
		in.IPv4 = nil
	}
	// Incus parity: the instance etag covers the whole object, so lifecycle
	// changes invalidate config/device edit forms too.
	in.configVersion++
	f.logOp(sp, fmt.Sprintf("%s instance %q", verb, name))
	return nil
}

func (f *Fake) DeleteInstance(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	if _, ok := sp.instances[name]; !ok {
		return notFound(name)
	}
	delete(sp.instances, name)
	f.logOp(sp, fmt.Sprintf("Deleting instance %q", name))
	return nil
}

func (f *Fake) CloneInstance(ctx context.Context, src, dst string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	from, ok := sp.instances[src]
	if !ok {
		return notFound(src)
	}
	if _, ok := sp.instances[dst]; ok {
		return conflict("instance %q already exists", dst)
	}
	// A real copy carries the whole instance (profiles, config, devices,
	// limits, snapshots); only identity and runtime state reset.
	inst := from.Instance
	inst.Name = dst
	inst.Status = backend.StatusStopped
	inst.IPv4 = nil
	inst.CreatedAt = f.now()
	inst.Profiles = slices.Clone(from.Profiles)
	sp.instances[dst] = &instance{
		Instance:  inst,
		snapshots: slices.Clone(from.snapshots),
		config:    maps.Clone(from.config),
		devices:   cloneDevices(from.devices),
		files:     cloneFiles(from.files),
	}
	f.logOp(sp, fmt.Sprintf("Cloning instance %q to %q", src, dst))
	return nil
}
