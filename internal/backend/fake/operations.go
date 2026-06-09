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
