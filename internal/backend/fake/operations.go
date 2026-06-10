package fake

import (
	"context"
	"fmt"

	"github.com/adam/lxcon/internal/backend"
)

// maxOps caps the fake's operation log so long-lived dev servers stay bounded.
const maxOps = 50

func (f *Fake) ListOperations(_ context.Context) ([]backend.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]backend.Operation(nil), f.ops...), nil
}

// CancelOperation cancels a running, cancelable operation, flipping it to a
// terminal "Cancelled" state. Unknown id → ErrNotFound; an operation the daemon
// would not cancel (already finished) → ErrInvalid, matching the real driver.
func (f *Fake) CancelOperation(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i := range f.ops {
		if f.ops[i].ID == id {
			if !f.ops[i].Cancelable {
				return invalid("operation %q is not cancelable", id)
			}
			f.ops[i].Status = "Cancelled"
			f.ops[i].Cancelable = false
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

	f.opSeq++
	id := fmt.Sprintf("op-%d", f.opSeq)
	f.ops = append([]backend.Operation{{
		ID:          id,
		Description: description,
		Class:       "task",
		Status:      "Running",
		Cancelable:  true,
		CreatedAt:   f.now(),
	}}, f.ops...)
	return id
}

// logOp records a completed task operation, newest first. The fake models
// "mutations produce operations" — every entry is an already-succeeded task,
// unlike Incus where running ops appear and completed ones are pruned.
// Callers must hold the mutex and only log from success paths.
func (f *Fake) logOp(description string) {
	f.opSeq++
	f.ops = append([]backend.Operation{{
		ID:          fmt.Sprintf("op-%d", f.opSeq),
		Description: description,
		Class:       "task",
		Status:      "Success",
		CreatedAt:   f.now(),
	}}, f.ops...)
	if len(f.ops) > maxOps {
		f.ops = f.ops[:maxOps]
	}
}
