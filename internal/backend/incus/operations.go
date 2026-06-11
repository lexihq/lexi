package incus

import (
	"context"
	"fmt"
	"sort"

	"github.com/adam/lxcon/internal/backend"
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

// CancelOperation asks the daemon to cancel a running operation. The daemon
// rejects a non-cancelable or finished operation with a 400 (mapped to
// ErrInvalid); an unknown id is ErrNotFound.
func (b *incusBackend) CancelOperation(ctx context.Context, id string) error {
	if err := b.project(ctx).DeleteOperation(id); err != nil {
		return fmt.Errorf("cancel operation %q: %w", id, mapErr(err))
	}
	return nil
}
