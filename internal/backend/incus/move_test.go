package incus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenameInstanceCallsThrough(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.RenameInstance(t.Context(), "demo", "renamed"))
	assert.Equal(t, [2]string{"demo", "renamed"}, s.renamedInstance)
}

func TestMoveInstanceSendsPool(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.MoveInstance(t.Context(), "demo", "zfs0"))
	require.NotNil(t, s.migratedInstance)
	assert.Equal(t, "demo", s.migratedInstance.Name)
	assert.Equal(t, "zfs0", s.migratedInstance.Pool)
	// The real client rejects MigrateInstance with Migration=false, so the pool
	// move must set it (the stub mirrors that guard below).
	assert.True(t, s.migratedInstance.Migration)
}
