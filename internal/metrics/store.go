// Package metrics retains a short rolling history of per-instance resource
// samples so the UI can render time-series charts. It is a server-side cache
// derived from the backend's existing point-in-time Metrics call, not a new
// backend capability.
package metrics

import (
	"sync"

	"github.com/lexihq/lexi/internal/backend"
)

// SeriesKey identifies a per-instance history buffer. Build one only via Key so
// the remote/project/instance scoping is applied consistently; the named type
// keeps a raw, unscoped string from reaching Append/Series by accident.
type SeriesKey string

// Store is a thread-safe ring buffer of MetricSamples keyed by SeriesKey. Each
// key keeps at most limit samples; appending beyond that evicts the oldest.
type Store struct {
	mu     sync.Mutex
	series map[SeriesKey][]backend.MetricSample
	limit  int
}

// NewStore returns a Store that keeps up to limit samples per key. A limit below
// 1 is clamped to 1, so the store always retains at least the latest sample and
// can never slice out of bounds.
func NewStore(limit int) *Store {
	if limit < 1 {
		limit = 1
	}
	return &Store{series: make(map[SeriesKey][]backend.MetricSample), limit: limit}
}

// Append records a sample for key. Samples whose timestamp does not strictly
// advance past the most recent one are ignored, so concurrent viewers polling
// the same instant cannot double-count.
func (s *Store) Append(key SeriesKey, sample backend.MetricSample) {
	s.mu.Lock()
	defer s.mu.Unlock()

	buf := s.series[key]
	if n := len(buf); n > 0 && !sample.Time.After(buf[n-1].Time) {
		return
	}
	buf = append(buf, sample)
	if len(buf) > s.limit {
		buf = buf[len(buf)-s.limit:]
	}
	s.series[key] = buf
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
