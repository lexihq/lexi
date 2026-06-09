package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
	"github.com/goccy/go-json"
	"github.com/gorilla/websocket"
)

// wsUpgrader upgrades console requests to WebSocket. The default same-origin
// check is sufficient: the terminal page is served from the same host.
var wsUpgrader = websocket.Upgrader{}

// console renders the full-page interactive terminal.
func (h handlers) console(w http.ResponseWriter, r *http.Request) {
	h.renderShell(w, r, http.StatusOK, ui.ConsolePage(h.backend.Capabilities(), r.PathValue("name")))
}

// consoleWS bridges a browser terminal to backend.Exec. Binary frames carry
// stdin/stdout bytes; text frames carry a {"cols","rows"} resize. A reader
// goroutine pumps client frames into the exec stdin pipe and resize channel
// while Exec writes stdout back as binary frames.
func (h handlers) consoleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote an error response.
	}
	defer closeAndLog("console WebSocket", conn)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	stdinR, stdinW := io.Pipe()
	resize := make(chan backend.WinSize, 1)

	go func() {
		defer cancel()
		defer closeAndLog("console stdin pipe", stdinW)
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			switch mt {
			case websocket.BinaryMessage:
				if _, err := stdinW.Write(data); err != nil {
					return
				}
			case websocket.TextMessage:
				var msg struct {
					Cols int `json:"cols"`
					Rows int `json:"rows"`
				}
				if err := json.Unmarshal(data, &msg); err != nil {
					continue
				}
				select {
				case resize <- backend.WinSize{Cols: msg.Cols, Rows: msg.Rows}:
				default: // drop a resize if one is already queued
				}
			}
		}
	}()

	err = h.backend.Exec(ctx, r.PathValue("name"), backend.ExecRequest{
		Stdin:  stdinR,
		Stdout: &wsConnWriter{conn: conn},
		Resize: resize,
	})
	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	if err != nil {
		closeMsg = websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())
	}
	if err := conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second)); err != nil {
		slog.Warn("write console WebSocket close message", "err", err)
	}
}

func closeAndLog(name string, closer io.Closer) {
	if err := closer.Close(); err != nil {
		slog.Warn("close console session", "name", name, "err", err)
	}
}

// wsConnWriter adapts a WebSocket connection to io.Writer, sending each write as
// one binary frame. It is the sole writer to the connection during a session.
type wsConnWriter struct {
	conn *websocket.Conn
}

func (w *wsConnWriter) Write(p []byte) (int, error) {
	if err := w.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// logs renders the console-log panel for an instance.
func (h handlers) logs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	log, err := h.backend.ConsoleLog(r.Context(), name)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.LogsPanel(name, log))
}
