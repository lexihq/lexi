package fake

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
)

func TestListOperationsEmptyInitially(t *testing.T) {
	ops, err := New().ListOperations(ctx())
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("expected no operations on a fresh fake, got %+v", ops)
	}
}

func TestMutationsRecordOperationsNewestFirst(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	if err := b.StartInstance(ctx(), "demo"); err != nil {
		t.Fatalf("start: %v", err)
	}

	ops, err := b.ListOperations(ctx())
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 operations (create, start), got %+v", ops)
	}
	if ops[0].Description != `Starting instance "demo"` {
		t.Errorf("newest first: got %q", ops[0].Description)
	}
	if ops[1].Description != `Creating instance "demo"` {
		t.Errorf("oldest last: got %q", ops[1].Description)
	}
	for _, op := range ops {
		if op.ID == "" || op.Status != "Success" || op.Class != "task" || op.CreatedAt.IsZero() {
			t.Errorf("incomplete operation: %+v", op)
		}
	}
}

func TestFailedMutationRecordsNoOperation(t *testing.T) {
	b := New()
	if err := b.StartInstance(ctx(), "ghost"); err == nil {
		t.Fatal("expected start of ghost to fail")
	}
	ops, err := b.ListOperations(ctx())
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("failed action must not record an operation, got %+v", ops)
	}
}

func TestOperationsLogCapped(t *testing.T) {
	b := New()
	for i := range 60 {
		mustCreate(t, b, fmt.Sprintf("i%d", i))
	}
	ops, err := b.ListOperations(ctx())
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	if len(ops) != 50 {
		t.Fatalf("expected the log capped at 50, got %d", len(ops))
	}
	if ops[0].Description != `Creating instance "i59"` {
		t.Errorf("newest entry wrong: %q", ops[0].Description)
	}
}

func TestCancelOperationMarksCancelled(t *testing.T) {
	b := New()
	id := b.SeedRunningOperation("Migrating instance \"demo\"")

	if err := b.CancelOperation(ctx(), id); err != nil {
		t.Fatalf("cancel operation: %v", err)
	}

	ops, err := b.ListOperations(ctx())
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	var found *backend.Operation
	for i := range ops {
		if ops[i].ID == id {
			found = &ops[i]
		}
	}
	if found == nil {
		t.Fatalf("cancelled operation missing from log")
	}
	if found.Status != "Cancelled" || found.Cancelable {
		t.Fatalf("operation not cancelled: %+v", found)
	}
}

func TestCancelOperationGhostIs404(t *testing.T) {
	err := New().CancelOperation(ctx(), "op-ghost")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestCancelOperationNotCancelableIsInvalid(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo") // records a Success (non-cancelable) op
	ops, err := b.ListOperations(ctx())
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	err = b.CancelOperation(ctx(), ops[0].ID)
	if !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("want ErrInvalid for a finished operation, got %v", err)
	}
}

func TestWatchOperationsTicksOnMutationAndClosesOnCancel(t *testing.T) {
	f := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := f.WatchOperations(ctx)
	if err != nil {
		t.Fatalf("WatchOperations: %v", err)
	}

	select {
	case <-ch:
		t.Fatal("unexpected tick before any mutation")
	default:
	}

	f.SeedRunningOperation("e2e seed")
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("no tick after operation was recorded")
	}

	cancel()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // closed, as required
			}
		case <-deadline:
			t.Fatal("channel not closed after ctx cancel")
		}
	}
}
