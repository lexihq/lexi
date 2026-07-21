package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsReturnsPanel(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), "GET", "/instances/demo/metrics", "", true)

	assert.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	assert.Contains(t, body, "Live metrics")
	assert.Contains(t, body, "256.0 MiB")
	assert.Contains(t, body, "12.5%")
}

func TestMetricsUnknownInstanceIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/instances/ghost/metrics", "", true)
	assert.Equal(t, http.StatusNotFound, res.Code)
}

func TestMetricsSeriesCoalescesRapidPolls(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))
	srv := New(b) // one server, so successive requests share the sample store

	var first metricsSeriesData
	require.NoError(t, json.Unmarshal(request(t, srv, "GET", "/instances/demo/metrics/series", "", false).Body.Bytes(), &first))
	require.Len(t, first.T, 1)
	require.NotNil(t, first.CPU[0])
	assert.InDelta(t, 12.5, *first.CPU[0], 0.001, "first sample is the canonical value")

	// A second poll lands within the store's minimum sample gap (e.g. the
	// background sampler and the chart poll interleaving): it must read the
	// history without inflating it, or the chart window halves.
	var second metricsSeriesData
	res := request(t, srv, "GET", "/instances/demo/metrics/series", "", false)
	assert.Equal(t, "application/json", res.Header().Get("Content-Type"))
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &second))
	require.Len(t, second.T, 1, "a poll inside the minimum sample gap must not add a sample")

	// Every series is mapped, not just CPU: a transposed/mislabeled field would
	// otherwise only surface visually in the chart. (Accumulation over spaced
	// samples is covered by the metrics package's store tests.)
	require.Len(t, second.MemUsed, 1)
	require.Len(t, second.Rx, 1)
	assert.Equal(t, int64(1024<<20), second.MemTotal[0], "memory total is reported")
	assert.Positive(t, second.Rx[0], "network counters are reported")
	assert.NotEqual(t, second.Rx[0], second.Tx[0], "rx and tx are distinct fields")
}

func TestMetricsSeriesUnknownInstanceIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/instances/ghost/metrics/series", "", false)
	assert.Equal(t, http.StatusNotFound, res.Code)
	assert.Equal(t, "application/json", res.Header().Get("Content-Type"), "JSON consumer gets a JSON error, not an HTML toast")
}

// TestMetricsSeriesIsolatesScopes guards the Key() scoping: same-named instances
// in different projects must keep independent histories, so one scope's polling
// never bleeds into another's chart.
func TestMetricsSeriesIsolatesScopes(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))
	require.NoError(t, b.CreateProject(t.Context(), backend.Project{Name: "dev", Description: ""}))
	require.NoError(t, b.CreateInstance(backend.WithProject(t.Context(), "dev"), backend.CreateOptions{Name: "demo", Image: "debian/12"}))
	srv := New(b)

	// Poll the default-scope instance twice, the dev-scope instance once.
	projectRequest(t, srv, "GET", "/instances/demo/metrics/series", "", "")
	projectRequest(t, srv, "GET", "/instances/demo/metrics/series", "", "")
	res := projectRequest(t, srv, "GET", "/instances/demo/metrics/series", "", "dev")

	var dev metricsSeriesData
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &dev))
	assert.Len(t, dev.T, 1, "dev scope must not see the default scope's two samples")
}
