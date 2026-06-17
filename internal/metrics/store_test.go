package metrics

import (
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
)

func sample(sec int, cpu float64) backend.MetricSample {
	return backend.MetricSample{
		Time:    time.Unix(int64(sec), 0),
		Metrics: backend.Metrics{CPUPercent: cpu},
	}
}

func TestStoreAppendAndSeries(t *testing.T) {
	s := NewStore(3)
	s.Append("demo", sample(1, 10))
	s.Append("demo", sample(2, 20))

	got := s.Series("demo")
	assert.Len(t, got, 2)
	assert.InDelta(t, 10.0, got[0].CPUPercent, 0.001)
	assert.InDelta(t, 20.0, got[1].CPUPercent, 0.001)
}

func TestStoreEvictsOldestBeyondCap(t *testing.T) {
	s := NewStore(2)
	s.Append("demo", sample(1, 10))
	s.Append("demo", sample(2, 20))
	s.Append("demo", sample(3, 30))

	got := s.Series("demo")
	assert.Len(t, got, 2, "ring buffer should cap at max")
	assert.InDelta(t, 20.0, got[0].CPUPercent, 0.001, "oldest sample should be dropped")
	assert.InDelta(t, 30.0, got[1].CPUPercent, 0.001)
}

func TestStoreClampsLimitBelowOne(t *testing.T) {
	// A non-positive limit must not panic or silently retain nothing: it is
	// clamped to 1 so the latest sample is always kept.
	for _, limit := range []int{0, -1} {
		s := NewStore(limit)
		s.Append("demo", sample(1, 10))
		s.Append("demo", sample(2, 20))

		got := s.Series("demo")
		assert.Len(t, got, 1, "limit %d should clamp to 1", limit)
		assert.InDelta(t, 20.0, got[0].CPUPercent, 0.001, "the latest sample is retained")
	}
}

func TestStoreDedupesNonAdvancingTimestamps(t *testing.T) {
	s := NewStore(5)
	s.Append("demo", sample(2, 20))
	s.Append("demo", sample(2, 99)) // same instant, ignored
	s.Append("demo", sample(1, 5))  // earlier, ignored

	got := s.Series("demo")
	assert.Len(t, got, 1)
	assert.InDelta(t, 20.0, got[0].CPUPercent, 0.001)
}

func TestStoreIsolatesKeys(t *testing.T) {
	s := NewStore(5)
	s.Append("a", sample(1, 10))
	s.Append("b", sample(1, 99))

	assert.Len(t, s.Series("a"), 1)
	assert.Len(t, s.Series("b"), 1)
	assert.InDelta(t, 10.0, s.Series("a")[0].CPUPercent, 0.001)
	assert.Empty(t, s.Series("missing"))
}

func TestStoreSeriesReturnsCopy(t *testing.T) {
	s := NewStore(5)
	s.Append("demo", sample(1, 10))

	got := s.Series("demo")
	got[0].CPUPercent = 0 // mutating the copy must not affect the store

	assert.InDelta(t, 10.0, s.Series("demo")[0].CPUPercent, 0.001)
}
