package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSamplerRecordsRunningInstances(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(context.Background(), backend.CreateOptions{Name: "up", Image: "debian/12"}))
	require.NoError(t, b.StartInstance(context.Background(), "up"))
	require.NoError(t, b.CreateInstance(context.Background(), backend.CreateOptions{Name: "down", Image: "debian/12"}))

	store := NewStore(10)
	s := NewSampler(b, store, time.Second)

	s.sampleOnce(context.Background())
	time.Sleep(time.Millisecond) // ensure the second tick's timestamp advances
	s.sampleOnce(context.Background())

	got := store.Series(Key(context.Background(), "up"))
	assert.Len(t, got, 2, "running instance should accumulate one sample per tick")
	assert.True(t, got[1].Time.After(got[0].Time), "samples should advance in time")

	assert.Empty(t, store.Series(Key(context.Background(), "down")), "stopped instance should not be sampled")
}

func TestSamplerRunStopsOnContextCancel(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(context.Background(), backend.CreateOptions{Name: "up", Image: "debian/12"}))
	require.NoError(t, b.StartInstance(context.Background(), "up"))

	store := NewStore(10)
	s := NewSampler(b, store, time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
