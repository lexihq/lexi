// Package server wires the HTTP routes for the lxcon web UI.
package server

import (
	"net/http"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
	"github.com/adam/lxcon/static"

	"github.com/a-h/templ"
)

// New builds an HTTP server with all lxcon routes registered. The backend is
// injected here so handlers stay driver-agnostic as the UI grows.
func New(_ backend.Backend) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static.FS)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("GET /", templ.Handler(ui.InstancesPage()))

	return &http.Server{Handler: mux}
}
