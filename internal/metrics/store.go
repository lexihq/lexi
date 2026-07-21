// Package metrics retains a short rolling history of per-instance resource
// samples so the UI can render time-series charts. It is a server-side cache
// derived from the backend's existing point-in-time Metrics call, not a new
// backend capability.
package metrics

import (
	"sync"
	"time"

	"github.com/lexihq/lexi/internal/backend"
)

// SeriesKey identifies a per-instance history buffer. Build one only via Key so
// the remote/project/instance scoping is applied consistently; the named type
// keeps a raw, unscoped string from reaching Append/Series by accident.
type SeriesKey string

// sweepEvery caps how often Append scans the whole map for expired keys, so
// eviction stays O(keys) once a minute instead of on every append.
const sweepEvery = time.Minute

// Store is a thread-safe ring buffer of MetricSamples keyed by SeriesKey. Each
// key keeps at most limit samples; appending beyond that evicts the oldest.
type Store struct {
	mu        sync.Mutex
	series    map[SeriesKey][]backend.MetricSample
	limit     int
	minGap    time.Duration
	maxAge    time.Duration
	lastSweep time.Time
}

// NewStore returns a Store that keeps up to limit samples per key. A limit
// below 1 is clamped to 1, so the store always retains at least the latest
// sample and can never slice out of bounds.
//
// minGap is the shortest interval between two retained samples of one key:
// several pollers reading the same instance (the background sampler plus a
// viewer's chart poll) would otherwise fill the ring at a multiple of the
// intended cadence, halving the history window. maxAge evicts keys that have
// not been appended to for that long — instances that were deleted or renamed
// would otherwise leave their buffers in the map forever. Zero disables the
// respective behavior.
func NewStore(limit int, minGap, maxAge time.Duration) *Store {
	if limit < 1 {
		limit = 1
	}
	return &Store{series: make(map[SeriesKey][]backend.MetricSample), limit: limit, minGap: minGap, maxAge: maxAge}
}

// Append records a sample for key. Samples whose timestamp does not advance at
// least minGap past the most recent one are ignored, so concurrent viewers
// polling the same instant (or interleaved with the background sampler) cannot
// inflate the ring's fill rate. The sample's own timestamp doubles as the
// clock for key expiry, keeping the store deterministic under test.
func (s *Store) Append(key SeriesKey, sample backend.MetricSample) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sweep(sample.Time)
	buf := s.series[key]
	if n := len(buf); n > 0 && !sample.Time.After(buf[n-1].Time.Add(s.minGap)) {
		return
	}
	buf = append(buf, sample)
	if len(buf) > s.limit {
		buf = buf[len(buf)-s.limit:]
	}
	s.series[key] = buf
}

// sweep drops keys whose newest sample is older than maxAge. Called under the
// lock, at most once per sweepEvery.
func (s *Store) sweep(now time.Time) {
	if s.maxAge <= 0 || now.Sub(s.lastSweep) < sweepEvery {
		return
	}
	s.lastSweep = now
	for key, buf := range s.series {
		if now.Sub(buf[len(buf)-1].Time) > s.maxAge {
			delete(s.series, key)
		}
	}
}

// Series returns a copy of the samples retained for key, oldest first.
func (s *Store) Series(key SeriesKey) []backend.MetricSample {
	s.mu.Lock()
	defer s.mu.Unlock()

	buf := s.series[key]
	if len(buf) == 0 {
		return nil
	}
	out := make([]backend.MetricSample, len(buf))
	copy(out, buf)
	return out
}
