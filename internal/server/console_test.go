package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsReturnsPanel(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), "GET", "/instances/demo/logs", "", true)

	assert.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	assert.Contains(t, body, "Console log")
	assert.Contains(t, body, "demo booted")
}

func TestLogsUnknownInstanceIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/instances/ghost/logs", "", true)
	assert.Equal(t, http.StatusNotFound, res.Code)
}

func TestConsolePageRenders(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12", Start: true}))

	res := request(t, New(b), "GET", "/instances/demo/console", "", false)

	assert.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	assert.Contains(t, body, "/static/js/xterm.js")
	assert.Contains(t, body, "/static/js/console.js")
	assert.Contains(t, body, "/instances/demo/console/ws")
	assert.Contains(t, body, `id="console-reconnect"`)
}

// A stopped instance gets a plain explanation instead of a dead terminal that
// silently fails to connect.
func TestConsolePageExplainsStoppedInstance(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), "GET", "/instances/demo/console", "", false)

	assert.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	assert.Contains(t, body, "the console needs it running")
	assert.NotContains(t, body, "/static/js/xterm.js")
}

func TestConsolePageKeepsInstanceTabs(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	inst, err := b.GetInstance(t.Context(), "demo")
	require.NoError(t, err)

	res := request(t, New(b), "GET", "/instances/demo/console", "", false)

	require.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	// The instance tab bar is present so navigation isn't lost on the console.
	assert.Contains(t, body, "/instances/demo?tab=snapshots")
	assert.Contains(t, body, "/instances/demo?tab=metrics")
	// Tabs are full-page navigations here, not #instance-body HTMX swaps.
	assert.NotContains(t, body, `hx-target="#instance-body"`)
	// The instance status is shown under the name, as on the detail page.
	assert.Contains(t, body, inst.Status)
}

func TestConsolePageUnknownInstanceIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/instances/ghost/console", "", false)
	assert.Equal(t, http.StatusNotFound, res.Code)
}

func TestConsoleWSBridgesExec(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	httpSrv := httptest.NewServer(New(b).Handler)
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/instances/demo/console/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})

	// Binary stdin is echoed back by the fake as binary stdout.
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("hello\n")))
	mt, data, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.BinaryMessage, mt)
	assert.Equal(t, "hello\n", string(data))

	// A resize control frame (text JSON) is accepted; the session stays live.
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"cols":100,"rows":30}`)))
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("world\n")))
	_, data, err = conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "world\n", string(data))
}
