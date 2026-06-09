package fake

import (
	"context"
	"maps"
	"sort"
	"strconv"

	"github.com/adam/lxcon/internal/backend"
)

func (f *Fake) GetServerOverview(_ context.Context) (backend.ServerOverview, error) {
	return backend.ServerOverview{
		ServerVersion: "6.0-fake",
		Kernel:        "Linux",
		KernelVersion: "6.1.0-fake",
		Driver:        "fake",
		DriverVersion: "1.0",
		CPUThreads:    8,
		MemoryUsed:    4 << 30,
		MemoryTotal:   16 << 30,
	}, nil
}

func (f *Fake) GetServerConfig(_ context.Context) (map[string]string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return maps.Clone(f.serverConfig), strconv.Itoa(f.serverConfigVersion), nil
}

func (f *Fake) UpdateServerConfig(_ context.Context, config map[string]string, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Empty version = unconditional, mirroring the Incus client's If-Match
	// semantics; a stale version means a concurrent writer landed first.
	if version != "" && version != strconv.Itoa(f.serverConfigVersion) {
		return conflict("server config version %s", version)
	}
	f.serverConfig = maps.Clone(config)
	if f.serverConfig == nil {
		f.serverConfig = map[string]string{}
	}
	f.serverConfigVersion++
	return nil
}

func (f *Fake) ListCertificates(_ context.Context) ([]backend.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]backend.Certificate(nil), f.certificates...), nil
}

func (f *Fake) ListWarnings(_ context.Context) ([]backend.Warning, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := append([]backend.Warning(nil), f.warnings...)
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	return out, nil
}

func (f *Fake) DeleteWarning(_ context.Context, uuid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i, w := range f.warnings {
		if w.UUID == uuid {
			f.warnings = append(f.warnings[:i], f.warnings[i+1:]...)
			return nil
		}
	}
	return notFoundf("warning %q", uuid)
}
