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
	// reading reflects ~one core fully busy over the elapsed second (≈100%).
	b.cpuSamples["demo"] = cpuSample{nanos: 1_000_000_000, at: time.Now().Add(-time.Second)}
	assert.Greater(t, b.cpuPercent("demo", 2_000_000_000, b.cpuEpochSnapshot()), 0.0)
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
		cpuSamples: map[string]cpuSample{"demo": {}},
	}
	epoch := b.cpuEpochSnapshot()

	b.clearCPUSample("demo")
	b.cpuPercent("demo", 1, epoch)

	assert.NotContains(t, b.cpuSamples, "demo")
}
