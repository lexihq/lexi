package server

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lexihq/lexi/internal/ui"
)

// operationsPanel renders the polled body of the bottom Tasks panel.
func (h handlers) operationsPanel(w http.ResponseWriter, r *http.Request) {
	ops, err := h.backend.ListOperations(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.OperationRows(ops))
}

// operationsEvents streams the Tasks panel body over SSE: one "operations"
// event immediately, then one per WatchOperations tick, with keepalive
// comments in between. Mid-stream failures just drop the connection — no
// status can be written after streaming starts, and EventSource reconnects.
func (h handlers) operationsEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ticks, err := h.backend.WatchOperations(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	send := func() bool {
		ops, err := h.backend.ListOperations(r.Context())
		if err != nil {
			return false
		}
		var buf bytes.Buffer
		if err := ui.OperationRows(ops).Render(r.Context(), &buf); err != nil {
			return false
		}
		if err := writeSSE(w, "operations", buf.String()); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	if !send() {
		return
	}

	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case _, open := <-ticks:
			if !open || !send() {
				return
			}
		case <-keepalive.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// writeSSE writes one named SSE event; every payload line must carry its own
// data: prefix or the browser truncates the event at the first newline.
func writeSSE(w io.Writer, event, data string) error {
	var sb strings.Builder
	sb.WriteString("event: ")
	sb.WriteString(event)
	sb.WriteString("\n")
	for line := range strings.SplitSeq(data, "\n") {
		sb.WriteString("data: ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	_, err := io.WriteString(w, sb.String())
	return err
}

// cancelOperation cancels a running operation, then re-renders the Tasks panel
// body so the status flips in place; a non-HTMX post redirects to the home page
// (the Tasks panel is an embedded partial — there is no standalone operations
// page to land on).
func (h handlers) cancelOperation(w http.ResponseWriter, r *http.Request) {
	if err := h.backend.CancelOperation(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, r, err)
		return
	}
	if !isHTMX(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.operationsPanel(w, r)
}
