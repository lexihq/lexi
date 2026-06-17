package incus

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/lxc/incus/v6/shared/api"

	"github.com/lexihq/lexi/internal/backend"
)

// ListOperations returns the daemon's running and recently finished
// operations, newest first.
func (b *incusBackend) ListOperations(ctx context.Context) ([]backend.Operation, error) {
	ops, err := b.project(ctx).GetOperations()
	if err != nil {
		return nil, fmt.Errorf("list operations: %w", mapErr(err))
	}
	out := make([]backend.Operation, 0, len(ops))
	for _, op := range ops {
		out = append(out, backend.Operation{
			ID:          op.ID,
			Description: op.Description,
			Class:       op.Class,
			Status:      op.Status,
			Err:         op.Err,
			CreatedAt:   op.CreatedAt,
			Cancelable:  op.MayCancel,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// WatchOperations subscribes to the daemon's events API and ticks the
// returned channel on every "operation" event. One listener per call; it is
// disconnected and the channel closed when ctx ends. The mutex serializes
// late handler fires against the close — the events client doesn't join
// in-flight handlers on Disconnect.
func (b *incusBackend) WatchOperations(ctx context.Context) (<-chan struct{}, error) {
	listener, err := b.project(ctx).GetEvents()
	if err != nil {
		return nil, fmt.Errorf("watch operations: %w", mapErr(err))
	}

	ch := make(chan struct{}, 1)
	var mu sync.Mutex
	closed := false
	_, err = listener.AddHandler([]string{api.EventTypeOperation}, func(api.Event) {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return
		}
		select {
		case ch <- struct{}{}:
		default: // a tick is already pending; coalesce
		}
	})
	if err != nil {
		listener.Disconnect()
		return nil, fmt.Errorf("watch operations: %w", mapErr(err))
	}

	go func() {
		<-ctx.Done()
		listener.Disconnect()
		mu.Lock()
		closed = true
		close(ch)
		mu.Unlock()
	}()
	return ch, nil
}

// CancelOperation asks the daemon to cancel a running operation. The daemon
// rejects a non-cancelable or finished operation with a 400 (mapped to
// ErrInvalid); an unknown id is ErrNotFound.
func (b *incusBackend) CancelOperation(ctx context.Context, id string) error {
	if err := b.project(ctx).DeleteOperation(id); err != nil {
		return fmt.Errorf("cancel operation %q: %w", id, mapErr(err))
	}
	return nil
}
