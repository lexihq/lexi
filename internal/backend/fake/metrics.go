package fake

import (
	"context"

	"github.com/adam/lxcon/internal/backend"
)

// Metrics returns deterministic canned counters for any existing instance, so
// handler and UI tests can assert the panel without a live daemon.
func (f *Fake) Metrics(_ context.Context, name string) (backend.Metrics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[name]; !ok {
		return backend.Metrics{}, notFound(name)
	}
	return backend.Metrics{
		CPUPercent:  12.5,
		MemoryUsage: 256 << 20,
		MemoryTotal: 1024 << 20,
		DiskUsage:   512 << 20,
		NetworkRx:   1 << 20,
		NetworkTx:   2 << 20,
		Processes:   7,
	}, nil
}
