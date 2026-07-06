package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/lexihq/lexi/internal/backend"
)

// Key scopes a store entry by remote, project, and instance name so that
// same-named instances in different projects or on different remotes keep
// separate histories. The \x00 separator cannot appear in those identifiers,
// so the parts can never collide.
func Key(ctx context.Context, name string) SeriesKey {
	return SeriesKey(backend.RemoteFromContext(ctx) + "\x00" + backend.ProjectFromContext(ctx) + "\x00" + name)
}

// Sampler periodically polls running instances and records their metrics into
// the Store, so charts have history even before a user opens the metrics tab.
// It runs in the default scope (the remote/project of the context passed to
// Run); instances viewed in another scope accumulate samples on demand via the
// series handler instead.
//
// Fields are unexported and set once by NewSampler: Run launches in a goroutine,
// so mutating its dependencies afterwards would be a data race.
type Sampler struct {
	backend  backend.Backend
	store    *Store
	interval time.Duration
}

// NewSampler builds a Sampler. A non-positive interval is clamped to one second
// so time.NewTicker in Run can never panic.
func NewSampler(b backend.Backend, store *Store, interval time.Duration) *Sampler {
	if interval <= 0 {
		interval = time.Second
	}
	return &Sampler{backend: b, store: store, interval: interval}
}

// Run samples on every interval tick until ctx is cancelled. Each tick recovers
// from a panic in the sampling path and logs it (see sampleOnce), so a transient
// backend bug skips one tick loudly rather than ending history collection for the
// whole process or taking it down.
func (s *Sampler) Run(ctx context.Context) {
	slog.Info("metrics sampler: started", "interval", s.interval)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("metrics sampler: stopped", "err", ctx.Err())
			return
		case <-ticker.C:
			s.sampleOnce(ctx)
		}
	}
}

// sampleOnce records one metrics sample for each running instance. It recovers
// from a panic in the sampling path so a single bad tick is contained: the loop
// in Run keeps sampling on the next tick instead of the goroutine unwinding and
// history collection stopping for the process lifetime.
func (s *Sampler) sampleOnce(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("metrics sampler: panic sampling tick, skipping", "panic", r)
		}
	}()
	instances, err := s.backend.ListInstances(ctx)
	if err != nil {
		// A list failure stalls the whole tick (no instance is sampled), so it
		// is more serious than a single instance's metrics failure below.
		slog.Error("metrics sampler: list instances", "err", err)
		return
	}
	now := time.Now()
	for _, inst := range instances {
		if inst.Status != backend.StatusRunning {
			continue
		}
		s.sampleInstance(ctx, inst.Name, now)
	}
}

// sampleInstance records one sample for a single instance, recovering from a
// panic so one instance's fault (e.g. a bad backend response) skips only that
// instance rather than aborting the tick and starving every instance sampled
// after it.
func (s *Sampler) sampleInstance(ctx context.Context, name string, now time.Time) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("metrics sampler: panic sampling instance, skipping", "instance", name, "panic", r)
		}
	}()
	m, err := s.backend.Metrics(ctx, name)
	if err != nil {
		slog.Warn("metrics sampler: fetch metrics", "instance", name, "err", err)
		return
	}
	s.store.Append(Key(ctx, name), backend.MetricSample{Time: now, Metrics: m})
}
