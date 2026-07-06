package incus

import (
	"context"
	"fmt"
	"time"

	"github.com/lexihq/lexi/internal/backend"
)

// cpuSample records a cumulative CPU-time reading so the next Metrics call can
// turn the delta into a CPU percentage. percent caches the last percentage
// computed for the key so a second poller sampling within cpuSampleFloor can
// reuse it instead of recomputing a delta over a near-zero interval.
type cpuSample struct {
	nanos   int64
	at      time.Time
	percent float64
}

// cpuSampleFloor is the shortest interval between two samples of a key that we
// treat as a real measurement window. Several ~3s pollers (the background
// sampler, the live metrics panel, and the chart-series poll) read the same
// instance, so two reads can land milliseconds apart; a delta over that gap is
// noise, so cpuPercent reuses the last percentage rather than reporting a
// spurious spike or a zero.
const cpuSampleFloor = 500 * time.Millisecond

// Metrics reads a point-in-time resource snapshot from the instance state.
// Disk usage is summed across devices and network counters across every
// interface except loopback. CPUPercent reads 0 until a prior sample exists.
func (b *incusBackend) Metrics(ctx context.Context, name string) (backend.Metrics, error) {
	epoch := b.cpuEpochSnapshot()
	state, _, err := b.project(ctx).GetInstanceState(name)
	if err != nil {
		return backend.Metrics{}, fmt.Errorf("get state of %q: %w", name, mapErr(err))
	}
	// Defensive at the boundary: the client can return a nil state with no error
	// (ipv4Addresses guards the same). Deref below would otherwise panic — in the
	// sampler goroutine or, via the metrics handler, the whole request.
	if state == nil {
		return backend.Metrics{}, fmt.Errorf("get state of %q: daemon returned no state", name)
	}
	m := backend.Metrics{
		MemoryUsage: state.Memory.Usage,
		MemoryTotal: state.Memory.Total,
		Processes:   state.Processes,
		CPUPercent:  b.cpuPercent(cpuSampleKey(ctx, name), state.CPU.Usage, epoch),
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

// cpuEpochSnapshot reads the current sample epoch under the lock; cpuPercent
// discards a reading taken against a stale epoch.
func (b *incusBackend) cpuEpochSnapshot() uint64 {
	b.cpuMu.Lock()
	defer b.cpuMu.Unlock()
	return b.cpuEpoch
}

// cpuSampleKey qualifies the instance name with the request's remote and
// project: one driver value serves every remote, and instances on different
// remotes (or in different projects) share names — a delta computed across
// scopes would be garbage.
func cpuSampleKey(ctx context.Context, name string) string {
	return backend.RemoteFromContext(ctx) + "/" + backend.ProjectFromContext(ctx) + "/" + name
}

// cpuPercent turns the delta between two cumulative CPU-time samples into a
// percentage. It records the new sample and returns 0 on the first reading or
// any non-positive interval.
func (b *incusBackend) cpuPercent(key string, cpuNanos int64, epoch uint64) float64 {
	now := time.Now()
	b.cpuMu.Lock()
	defer b.cpuMu.Unlock()
	// Evict stale samples at most once per TTL instead of scanning the whole map
	// on every call: each sampler tick calls this once per running instance, so a
	// per-call sweep is O(N²) under the lock.
	if now.Sub(b.cpuLastSweep) > cpuSampleTTL {
		for sampleName, sample := range b.cpuSamples {
			if now.Sub(sample.at) > cpuSampleTTL {
				delete(b.cpuSamples, sampleName)
			}
		}
		b.cpuLastSweep = now
	}
	if epoch != b.cpuEpoch {
		return 0
	}
	prev, ok := b.cpuSamples[key]
	if !ok {
		b.cpuSamples[key] = cpuSample{nanos: cpuNanos, at: now}
		return 0
	}
	elapsed := now.Sub(prev.at)
	if elapsed < cpuSampleFloor {
		// Another poller sampled this key a moment ago; a delta over this gap is
		// noise. Reuse the last percentage and keep prev as the baseline so the
		// next real interval measures against it.
		return prev.percent
	}
	pct := 0.0
	if delta := cpuNanos - prev.nanos; delta >= 0 {
		pct = float64(delta) / float64(elapsed.Nanoseconds()) * 100
	}
	b.cpuSamples[key] = cpuSample{nanos: cpuNanos, at: now, percent: pct}
	return pct
}

func (b *incusBackend) clearCPUSample(key string) {
	b.cpuMu.Lock()
	defer b.cpuMu.Unlock()
	delete(b.cpuSamples, key)
	b.cpuEpoch++
}
