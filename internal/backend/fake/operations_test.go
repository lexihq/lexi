package fake

import (
	"fmt"
	"testing"
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
