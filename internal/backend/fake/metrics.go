package fake

import (
	"context"

	"github.com/adam/lxcon/internal/backend"
)

// Metrics returns deterministic canned counters for any existing instance, so
// handler and UI tests can assert the panel without a live daemon.
func (f *Fake) Metrics(ctx context.Context, name string) (backend.Metrics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[name]
	if !ok {
		return backend.Metrics{}, notFound(name)
	}
	// Vary the sample with each call so the history charts show movement; tick 0
	// returns the canonical values the metrics tests assert on. Network counters
	// climb monotonically, mirroring real cumulative byte counters.
	n := int64(in.metricsTick)
	in.metricsTick++
	return backend.Metrics{
		CPUPercent:  12.5 + float64((n*7)%40),
		MemoryUsage: (256 + (n%8)*16) << 20,
		MemoryTotal: 1024 << 20,
		DiskUsage:   (512 + (n%4)*8) << 20,
		NetworkRx:   (1 + n) << 20,
		NetworkTx:   (2 + n) << 20,
		Processes:   7,
	}, nil
}
