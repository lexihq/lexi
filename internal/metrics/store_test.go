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
		Metrics: backend.Metrics{CPUPercent: &cpu},
	}
}

func TestStoreAppendAndSeries(t *testing.T) {
	s := NewStore(3, 0, 0)
	s.Append("demo", sample(1, 10))
	s.Append("demo", sample(2, 20))

	got := s.Series("demo")
	assert.Len(t, got, 2)
	assert.InDelta(t, 10.0, *got[0].CPUPercent, 0.001)
	assert.InDelta(t, 20.0, *got[1].CPUPercent, 0.001)
}

func TestStoreEvictsOldestBeyondCap(t *testing.T) {
	s := NewStore(2, 0, 0)
	s.Append("demo", sample(1, 10))
	s.Append("demo", sample(2, 20))
	s.Append("demo", sample(3, 30))

	got := s.Series("demo")
	assert.Len(t, got, 2, "ring buffer should cap at max")
	assert.InDelta(t, 20.0, *got[0].CPUPercent, 0.001, "oldest sample should be dropped")
	assert.InDelta(t, 30.0, *got[1].CPUPercent, 0.001)
}

func TestStoreClampsLimitBelowOne(t *testing.T) {
	// A non-positive limit must not panic or silently retain nothing: it is
	// clamped to 1 so the latest sample is always kept.
	for _, limit := range []int{0, -1} {
		s := NewStore(limit, 0, 0)
		s.Append("demo", sample(1, 10))
		s.Append("demo", sample(2, 20))

		got := s.Series("demo")
		assert.Len(t, got, 1, "limit %d should clamp to 1", limit)
		assert.InDelta(t, 20.0, *got[0].CPUPercent, 0.001, "the latest sample is retained")
	}
}

func TestStoreDedupesNonAdvancingTimestamps(t *testing.T) {
	s := NewStore(5, 0, 0)
	s.Append("demo", sample(2, 20))
	s.Append("demo", sample(2, 99)) // same instant, ignored
	s.Append("demo", sample(1, 5))  // earlier, ignored

	got := s.Series("demo")
	assert.Len(t, got, 1)
	assert.InDelta(t, 20.0, *got[0].CPUPercent, 0.001)
}

func TestStoreMinGapCollapsesInterleavedPollers(t *testing.T) {
	// Sampler at t=0,3,6 and a chart poll at t=1.5,4.5: with a 2s floor only
	// the sampler cadence survives, so two pollers cannot halve the window.
	s := NewStore(10, 2*time.Second, 0)
	at := func(ms int, cpu float64) backend.MetricSample {
		return backend.MetricSample{
			Time:    time.Unix(0, int64(ms)*int64(time.Millisecond)).Add(time.Hour),
			Metrics: backend.Metrics{CPUPercent: &cpu},
		}
	}
	s.Append("demo", at(0, 1))
	s.Append("demo", at(1500, 2)) // within the gap, ignored
	s.Append("demo", at(3000, 3))
	s.Append("demo", at(4500, 4)) // within the gap, ignored
	s.Append("demo", at(6000, 5))

	got := s.Series("demo")
	assert.Len(t, got, 3)
	assert.InDelta(t, 1.0, *got[0].CPUPercent, 0.001)
	assert.InDelta(t, 3.0, *got[1].CPUPercent, 0.001)
	assert.InDelta(t, 5.0, *got[2].CPUPercent, 0.001)
}

func TestStoreEvictsIdleKeys(t *testing.T) {
	// A key nothing appends to (deleted instance) is dropped once its newest
	// sample ages past maxAge; the still-active key survives the sweep.
	s := NewStore(10, 0, time.Hour)
	base := time.Unix(0, 0).Add(24 * time.Hour)
	s.Append("dead", backend.MetricSample{Time: base})
	s.Append("live", backend.MetricSample{Time: base.Add(61 * time.Minute)})

	assert.Empty(t, s.Series("dead"), "idle key should be evicted by the sweep")
	assert.Len(t, s.Series("live"), 1)
}

func TestStoreSweepIsRateLimited(t *testing.T) {
	// maxAge shorter than the sweep cadence: the idle key outlives its maxAge
	// until the next scan window opens, proving appends between windows do not
	// perform a scan each.
	s := NewStore(10, 0, 10*time.Second)
	base := time.Unix(0, 0).Add(24 * time.Hour)
	app := func(key SeriesKey, at time.Time) {
		s.Append(key, backend.MetricSample{Time: at})
	}
	app("live", base) // first append: a sweep runs, the window starts
	app("dead", base.Add(5*time.Second))
	app("live", base.Add(30*time.Second)) // dead is past maxAge, but inside the window: no scan
	assert.Len(t, s.Series("dead"), 1, "no sweep inside the rate-limit window")

	app("live", base.Add(70*time.Second)) // window passed: the scan evicts
	assert.Empty(t, s.Series("dead"))
}

func TestStoreIsolatesKeys(t *testing.T) {
	s := NewStore(5, 0, 0)
	s.Append("a", sample(1, 10))
	s.Append("b", sample(1, 99))

	assert.Len(t, s.Series("a"), 1)
	assert.Len(t, s.Series("b"), 1)
	assert.InDelta(t, 10.0, *s.Series("a")[0].CPUPercent, 0.001)
	assert.Empty(t, s.Series("missing"))
}

func TestStoreSeriesReturnsCopy(t *testing.T) {
	s := NewStore(5, 0, 0)
	s.Append("demo", sample(1, 10))

	got := s.Series("demo")
	got[0].CPUPercent = nil // mutating the copy must not affect the store

	assert.InDelta(t, 10.0, *s.Series("demo")[0].CPUPercent, 0.001)
}
