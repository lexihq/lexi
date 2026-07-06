package incus

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCPUPercentZeroOnFirstSampleThenDeltaBased(t *testing.T) {
	b := &incusBackend{cpuSamples: make(map[string]cpuSample)}

	// First reading has no prior sample, so it reads 0.
	assert.Zero(t, b.cpuPercent("demo", 1_000_000_000, b.cpuEpochSnapshot()))

	// Pre-seed a sample one second in the past with 1e9 fewer nanos so the next
	// reading reflects ~one core fully busy over the elapsed second (≈100%). The
	// interval must exceed cpuSampleFloor for the delta (not the reused percent)
	// to be returned.
	b.cpuSamples["demo"] = cpuSample{nanos: 1_000_000_000, at: time.Now().Add(-time.Second)}
	assert.Greater(t, b.cpuPercent("demo", 2_000_000_000, b.cpuEpochSnapshot()), 0.0)
}

func TestCPUPercentReusesLastPercentWithinFloor(t *testing.T) {
	b := &incusBackend{cpuSamples: map[string]cpuSample{
		// A prior sample taken well under cpuSampleFloor ago, with a cached percent.
		"demo": {nanos: 1_000_000_000, at: time.Now().Add(-10 * time.Millisecond), percent: 42},
	}}

	// A second poller reads a few ms later: the delta over that gap is noise, so
	// the cached percentage is reused verbatim rather than recomputed into a
	// spike/zero.
	assert.InDelta(t, 42.0, b.cpuPercent("demo", 5_000_000_000, b.cpuEpochSnapshot()), 1e-9)
}

func TestCPUPercentPrunesStaleSamples(t *testing.T) {
	b := &incusBackend{
		cpuSamples: map[string]cpuSample{
			"deleted": {at: time.Now().Add(-cpuSampleTTL - time.Second)},
		},
	}

	b.cpuPercent("active", 1, b.cpuEpochSnapshot())

	assert.NotContains(t, b.cpuSamples, "deleted")
	assert.Contains(t, b.cpuSamples, "active")
}

func TestCPUPercentDoesNotRecreateSampleAfterDeletion(t *testing.T) {
	b := &incusBackend{
		cpuSamples: map[string]cpuSample{"/demo": {}},
	}
	epoch := b.cpuEpochSnapshot()

	b.clearCPUSample("/demo")
	b.cpuPercent("demo", 1, epoch)

	assert.NotContains(t, b.cpuSamples, "/demo")
}
