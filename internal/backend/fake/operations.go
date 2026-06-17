package fake

import (
	"context"
	"fmt"

	"github.com/lexihq/lexi/internal/backend"
)

// WatchOperations registers a watcher that receives a tick whenever any
// operation is recorded or changed (all projects — re-rendering is scoped by
// the caller). The channel is buffered size 1 so bursts coalesce, and is
// closed once ctx ends.
func (f *Fake) WatchOperations(ctx context.Context) (<-chan struct{}, error) {
	ch := make(chan struct{}, 1)
	f.mu.Lock()
	if f.opWatchers == nil {
		f.opWatchers = map[int]chan struct{}{}
	}
	f.opWatcherID++
	id := f.opWatcherID
	f.opWatchers[id] = ch
	f.mu.Unlock()

	go func() {
		<-ctx.Done()
		f.mu.Lock()
		delete(f.opWatchers, id)
		f.mu.Unlock()
		close(ch)
	}()
	return ch, nil
}

// notifyOpWatchers ticks every registered watcher without blocking; callers
// must hold the mutex (sends never race the close because unregistration also
// holds it).
func (f *Fake) notifyOpWatchers() {
	for _, ch := range f.opWatchers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// maxOps caps the fake's operation log so long-lived dev servers stay bounded.
const maxOps = 50

func (f *Fake) ListOperations(ctx context.Context) ([]backend.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	return append([]backend.Operation(nil), sp.ops...), nil
}

// CancelOperation cancels a running, cancelable operation, flipping it to a
// terminal "Cancelled" state. Unknown id → ErrNotFound; an operation the daemon
// would not cancel (already finished) → ErrInvalid, matching the real driver.
func (f *Fake) CancelOperation(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	for i := range sp.ops {
		if sp.ops[i].ID == id {
			if !sp.ops[i].Cancelable {
				return invalid("operation %q is not cancelable", id)
			}
			sp.ops[i].Status = "Cancelled"
			sp.ops[i].Cancelable = false
			f.notifyOpWatchers()
			return nil
		}
	}
	return notFoundf("operation %q", id)
}

// SeedRunningOperation records a cancelable running operation and returns its
// ID. The fake's normal mutation log only contains already-succeeded tasks, so
// tests and the e2e fakeserver use this to exercise the cancel path.
func (f *Fake) SeedRunningOperation(description string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Seeds always land on the local remote's default project.
	sp := f.remoteFor("local").spaceFor("default")

	sp.opSeq++
	id := fmt.Sprintf("op-%d", sp.opSeq)
	sp.ops = append([]backend.Operation{{
		ID:          id,
		Description: description,
		Class:       "task",
		Status:      "Running",
		Cancelable:  true,
		CreatedAt:   f.now(),
	}}, sp.ops...)
	f.notifyOpWatchers()
	return id
}

// logOp records a completed task operation, newest first. The fake models
// "mutations produce operations" — every entry is an already-succeeded task,
// unlike Incus where running ops appear and completed ones are pruned.
// Callers must hold the mutex and only log from success paths.
func (f *Fake) logOp(sp *space, description string) {
	sp.opSeq++
	sp.ops = append([]backend.Operation{{
		ID:          fmt.Sprintf("op-%d", sp.opSeq),
		Description: description,
		Class:       "task",
		Status:      "Success",
		CreatedAt:   f.now(),
	}}, sp.ops...)
	if len(sp.ops) > maxOps {
		sp.ops = sp.ops[:maxOps]
	}
	f.notifyOpWatchers()
}
