package incus

import (
	"context"
	"fmt"
	"sort"

	"github.com/adam/lxcon/internal/backend"
)

// ListOperations returns the daemon's running and recently finished
// operations, newest first.
func (b *incusBackend) ListOperations(_ context.Context) ([]backend.Operation, error) {
	ops, err := b.srv.GetOperations()
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
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
