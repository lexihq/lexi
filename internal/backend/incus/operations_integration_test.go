//go:build integration

package incus

import (
	"context"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
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

	var seen bool
	for !seen {
		select {
		case err := <-done:
			require.NoError(t, err)
			if !seen {
				// Final look after completion: finished ops linger briefly.
				ops, lerr := b.ListOperations(ctx)
				require.NoError(t, lerr)
				seen = len(ops) > 0
			}
			require.True(t, seen, "no operation observed during instance creation")
			return
		default:
			ops, err := b.ListOperations(ctx)
			require.NoError(t, err)
			seen = len(ops) > 0
			if !seen {
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
	require.NoError(t, <-done)
}
