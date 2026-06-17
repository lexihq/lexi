package fake

import (
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetrics(t *testing.T) {
	b := New()
	assert.True(t, b.Capabilities(ctx()).Metrics, "fake should advertise metrics")
	mustCreate(t, b, "demo")

	m, err := b.Metrics(ctx(), "demo")
	require.NoError(t, err)
	assert.Positive(t, m.MemoryUsage)
	assert.Positive(t, m.MemoryTotal)

	_, err = b.Metrics(ctx(), "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)
}
