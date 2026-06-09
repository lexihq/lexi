package incus

import (
	"context"
	"fmt"
	"time"

	"github.com/adam/lxcon/internal/backend"
)

// cpuSample records a cumulative CPU-time reading so the next Metrics call can
// turn the delta into a CPU percentage.
type cpuSample struct {
	nanos int64
	at    time.Time
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

func (b *incusBackend) clearCPUSample(name string) {
	b.cpuMu.Lock()
	defer b.cpuMu.Unlock()
	delete(b.cpuSamples, name)
	b.cpuEpoch++
}
