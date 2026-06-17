//go:build integration

package incus

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/require"
)

// TestListOperationsObservesRunningOperation polls the operations list while an
// instance create runs, asserting the in-flight (or just-finished — completed
// ops linger a few seconds) operation is visible. Deliberately not a skip-prone
// "list and hope": the create guarantees an operation exists to observe.
func TestListOperationsObservesRunningOperation(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("ops")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	done := make(chan error, 1)
	go func() {
		done <- b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage})
	}()

	// seesInstanceOp filters to instance operations so unrelated background
	// daemon activity can't satisfy the assertion.
	seesInstanceOp := func() bool {
		ops, err := b.ListOperations(ctx)
		require.NoError(t, err)
		for _, op := range ops {
			if strings.Contains(strings.ToLower(op.Description), "instance") {
				return true
			}
		}
		return false
	}

	var seen bool
	for !seen {
		select {
		case err := <-done:
			require.NoError(t, err)
			if !seen {
				// Final look after completion: finished ops linger briefly.
				seen = seesInstanceOp()
			}
			require.True(t, seen, "no instance operation observed during instance creation")
			return
		default:
			seen = seesInstanceOp()
			if !seen {
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
	require.NoError(t, <-done)
}

// TestCancelOperationGhostErrors exercises the real DeleteOperation error
// mapping: cancelling a non-existent operation surfaces as ErrNotFound rather
// than a raw client error. A deterministic positive cancel would need a
// guaranteed long-lived cancelable operation, which the daemon doesn't offer
// cheaply, so the cancel path's happy case is covered by the fake + handler.
func TestCancelOperationGhostErrors(t *testing.T) {
	b := newBackend(t)
	err := b.CancelOperation(context.Background(), "00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

// TestWatchOperationsTicksOnInstanceCreate subscribes to the events API and
// asserts a tick arrives while an instance create runs — the create guarantees
// at least one operation event. The channel must close after ctx cancel.
func TestWatchOperationsTicksOnInstanceCreate(t *testing.T) {
	b := newBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	name := uniqueName("evw")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	ticks, err := b.WatchOperations(ctx)
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		done <- b.CreateInstance(context.Background(), backend.CreateOptions{Name: name, Image: testImage})
	}()

	select {
	case _, open := <-ticks:
		require.True(t, open, "tick channel closed before cancel")
	case <-time.After(2 * time.Minute):
		t.Fatal("no operation event tick during instance create")
	}
	require.NoError(t, <-done)

	cancel()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, open := <-ticks:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("tick channel not closed after ctx cancel")
		}
	}
}
