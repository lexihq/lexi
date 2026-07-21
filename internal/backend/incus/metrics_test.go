package incus

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCPUPercentZeroOnFirstSampleThenDeltaBased(t *testing.T) {
	b := &incusBackend{cpuSamples: make(map[string]cpuSample)}

	// First reading has no prior sample: no measurement window yet.
	pct, ok := b.cpuPercent("demo", 1_000_000_000, time.Now())
	assert.Zero(t, pct)
	assert.False(t, ok)

	// Pre-seed a sample one second in the past with 1e9 fewer nanos so the next
	// reading reflects ~one core fully busy over the elapsed second (≈100%). The
	// interval must exceed cpuSampleFloor for the delta (not the reused percent)
	// to be returned.
	b.cpuSamples["demo"] = cpuSample{nanos: 1_000_000_000, at: time.Now().Add(-time.Second)}
	pct, ok = b.cpuPercent("demo", 2_000_000_000, time.Now())
	assert.Greater(t, pct, 0.0)
	assert.True(t, ok)
}

func TestCPUPercentReusesLastPercentWithinFloor(t *testing.T) {
	b := &incusBackend{cpuSamples: map[string]cpuSample{
		// A prior sample taken well under cpuSampleFloor ago, with a cached percent.
		"demo": {nanos: 1_000_000_000, at: time.Now().Add(-10 * time.Millisecond), percent: 42, measured: true},
	}}

	// A second poller reads a few ms later: the delta over that gap is noise, so
	// the cached percentage is reused verbatim rather than recomputed into a
	// spike/zero.
	pct, ok := b.cpuPercent("demo", 5_000_000_000, time.Now())
	assert.InDelta(t, 42.0, pct, 1e-9)
	assert.True(t, ok)
}

func TestCPUPercentPrunesStaleSamples(t *testing.T) {
	b := &incusBackend{
		cpuSamples: map[string]cpuSample{
			"deleted": {at: time.Now().Add(-cpuSampleTTL - time.Second)},
		},
	}

	_, _ = b.cpuPercent("active", 1, time.Now())

	assert.NotContains(t, b.cpuSamples, "deleted")
	assert.Contains(t, b.cpuSamples, "active")
}

func TestCPUPercentDiscardsReadStartedBeforeClear(t *testing.T) {
	b := &incusBackend{
		cpuSamples: map[string]cpuSample{"/demo": {nanos: 1_000_000_000, at: time.Now().Add(-time.Second)}},
	}

	// A Metrics call began its state read, then the instance was deleted: the
	// in-flight reading may carry the dead instance's CPU time, so it must not
	// re-seed a baseline for the key.
	readStart := time.Now()
	b.clearCPUSample("/demo")
	pct, ok := b.cpuPercent("/demo", 1, readStart)
	assert.Zero(t, pct)
	assert.False(t, ok, "a stale read has no valid measurement")
	assert.False(t, b.cpuSamples["/demo"].clearedAt.IsZero(), "the tombstone must survive the stale read")
}

func TestCPUPercentReseedsAfterClear(t *testing.T) {
	b := &incusBackend{cpuSamples: map[string]cpuSample{"/demo": {nanos: 1_000_000_000, at: time.Now().Add(-time.Second)}}}

	b.clearCPUSample("/demo")

	// A read that started after the clear belongs to the key's next life
	// (e.g. a re-created instance): it re-seeds the baseline like a fresh key.
	pct, ok := b.cpuPercent("/demo", 2_000_000_000, time.Now())
	assert.Zero(t, pct)
	assert.False(t, ok)
	got := b.cpuSamples["/demo"]
	assert.True(t, got.clearedAt.IsZero(), "the tombstone is replaced by a live baseline")
	assert.EqualValues(t, 2_000_000_000, got.nanos)
}

func TestClearCPUSampleDoesNotInvalidateOtherKeys(t *testing.T) {
	b := &incusBackend{cpuSamples: map[string]cpuSample{
		"/other": {nanos: 1_000_000_000, at: time.Now().Add(-time.Second)},
	}}

	// Deleting one instance must not zero a concurrent reading of another —
	// the old global epoch did exactly that for one tick.
	readStart := time.Now()
	b.clearCPUSample("/deleted")
	pct, ok := b.cpuPercent("/other", 2_000_000_000, readStart)
	assert.Greater(t, pct, 0.0)
	assert.True(t, ok)
}
