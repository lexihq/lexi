package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/gorilla/websocket"
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
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), "GET", "/instances/demo/console", "", false)

	assert.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	assert.Contains(t, body, "/static/js/xterm.js")
	assert.Contains(t, body, "/static/js/console.js")
	assert.Contains(t, body, "/instances/demo/console/ws")
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
