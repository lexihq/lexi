package incus

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

// GetServerOverview combines the daemon environment with the host's headline
// resources (CPU threads, memory).
func (b *incusBackend) GetServerOverview(_ context.Context) (backend.ServerOverview, error) {
	srv, _, err := b.srv.GetServer()
	if err != nil {
		return backend.ServerOverview{}, fmt.Errorf("get server: %w", mapErr(err))
	}
	res, err := b.srv.GetServerResources()
	if err != nil {
		return backend.ServerOverview{}, fmt.Errorf("get server resources: %w", mapErr(err))
	}
	env := srv.Environment
	return backend.ServerOverview{
		ServerVersion: env.ServerVersion,
		Kernel:        env.Kernel,
		KernelVersion: env.KernelVersion,
		Driver:        env.Driver,
		DriverVersion: env.DriverVersion,
		CPUThreads:    int(res.CPU.Total),      //nolint:gosec // G115: CPU thread counts are tiny.
		MemoryUsed:    int64(res.Memory.Used),  //nolint:gosec // G115: real memory sizes fit int64.
		MemoryTotal:   int64(res.Memory.Total), //nolint:gosec // G115: real memory sizes fit int64.
	}, nil
}

func (b *incusBackend) GetServerConfig(_ context.Context) (map[string]string, error) {
	srv, _, err := b.srv.GetServer()
	if err != nil {
		return nil, fmt.Errorf("get server config: %w", mapErr(err))
	}
	return maps.Clone(map[string]string(srv.Config)), nil
}

// UpdateServerConfig replaces the server config map (GET-then-PUT with etag).
func (b *incusBackend) UpdateServerConfig(ctx context.Context, config map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, etag, err := b.srv.GetServer()
	if err != nil {
		return fmt.Errorf("get server: %w", mapErr(err))
	}
	if err := b.srv.UpdateServer(api.ServerPut{Config: api.ConfigMap(config)}, etag); err != nil {
		return fmt.Errorf("update server config: %w", mapErr(err))
	}
	return nil
}

func (b *incusBackend) ListCertificates(_ context.Context) ([]backend.Certificate, error) {
	certs, err := b.srv.GetCertificates()
	if err != nil {
		return nil, fmt.Errorf("list certificates: %w", mapErr(err))
	}
	out := make([]backend.Certificate, 0, len(certs))
	for _, c := range certs {
		out = append(out, backend.Certificate{
			Name:        c.Name,
			Type:        c.Type,
			Fingerprint: c.Fingerprint,
			Restricted:  c.Restricted,
		})
	}
	return out, nil
}

// ListWarnings returns daemon warnings, newest last-seen first.
func (b *incusBackend) ListWarnings(_ context.Context) ([]backend.Warning, error) {
	warnings, err := b.srv.GetWarnings()
	if err != nil {
		return nil, fmt.Errorf("list warnings: %w", mapErr(err))
	}
	out := make([]backend.Warning, 0, len(warnings))
	for _, w := range warnings {
		out = append(out, backend.Warning{
			UUID:        w.UUID,
			Type:        w.Type,
			Severity:    w.Severity,
			Status:      w.Status,
			Count:       w.Count,
			LastMessage: w.LastMessage,
			LastSeenAt:  w.LastSeenAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	return out, nil
}

func (b *incusBackend) DeleteWarning(_ context.Context, uuid string) error {
	if err := b.srv.DeleteWarning(uuid); err != nil {
		return fmt.Errorf("delete warning %q: %w", uuid, mapErr(err))
	}
	return nil
}
