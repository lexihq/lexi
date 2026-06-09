package incus

import (
	"context"
	"testing"
	"time"

	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListOperationsMapsAndSortsNewestFirst(t *testing.T) {
	older := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	newer := older.Add(time.Minute)
	b := &incusBackend{srv: &instanceServerStub{
		operations: []api.Operation{
			{
				ID:          "op-old",
				Class:       "task",
				Description: "Creating instance",
				Status:      "Success",
				CreatedAt:   older,
			},
			{
				ID:          "op-new",
				Class:       "websocket",
				Description: "Executing command",
				Status:      "Failure",
				Err:         "boom",
				CreatedAt:   newer,
			},
		},
	}}

	got, err := b.ListOperations(context.Background())

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "op-new", got[0].ID)
	assert.Equal(t, "websocket", got[0].Class)
	assert.Equal(t, "Executing command", got[0].Description)
	assert.Equal(t, "Failure", got[0].Status)
	assert.Equal(t, "boom", got[0].Err)
	assert.Equal(t, newer, got[0].CreatedAt)
	assert.Equal(t, "op-old", got[1].ID)
}
