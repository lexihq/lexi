package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationsPartialListsRecordedTasks(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), "GET", "/partials/operations", "", true)

	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "Creating instance")
	assert.Contains(t, res.Body.String(), "Success")
}

func TestOperationsPartialEmptyState(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/partials/operations", "", true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "No recent tasks")
}

func TestCancelOperationCancelsAndReturnsRows(t *testing.T) {
	b := fake.New()
	id := b.SeedRunningOperation(`Migrating instance "demo"`)

	res := formRequest(t, New(b), "/operations/"+id+"/cancel", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), `id="operations"`)
	assert.Contains(t, res.Body.String(), "Cancelled")

	ops, err := b.ListOperations(t.Context())
	require.NoError(t, err)
	require.Equal(t, "Cancelled", ops[0].Status)
}

func TestCancelOperationGhostIs404(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/operations/op-ghost/cancel", url.Values{}, true)
	assertStatus(t, res, http.StatusNotFound)
}

// readSSEFrame reads one named SSE event from r: the event line, its data:
// lines joined, skipping keepalive comments. Fails the test on stream end.
func readSSEFrame(t *testing.T, r *bufio.Reader) (event, data string) {
	t.Helper()
	var lines []string
	for {
		line, err := r.ReadString('\n')
		require.NoError(t, err, "SSE stream ended early")
		line = strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(line, ":"): // keepalive comment
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			lines = append(lines, strings.TrimPrefix(line, "data: "))
		case line == "" && (event != "" || len(lines) > 0):
			return event, strings.Join(lines, "\n")
		}
	}
}

func TestOperationsEventsStreamsPanelFrames(t *testing.T) {
	b := fake.New()
	ts := httptest.NewServer(New(b).Handler)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/events/operations", nil)
	require.NoError(t, err)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()

	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, res.Header.Get("Content-Type"), "text/event-stream")

	r := bufio.NewReader(res.Body)
	event, data := readSSEFrame(t, r)
	assert.Equal(t, "operations", event)
	assert.Contains(t, data, "No recent tasks")

	b.SeedRunningOperation("sse stream op")
	event, data = readSSEFrame(t, r)
	assert.Equal(t, "operations", event)
	assert.Contains(t, data, "sse stream op")
}
