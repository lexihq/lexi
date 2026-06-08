// Package server wires the HTTP routes for the lxcon web UI.
package server

import (
	"net/http"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/static"
)

// New builds an HTTP server with all lxcon routes registered. The backend is
// injected here so handlers stay driver-agnostic as the UI grows.
func New(b backend.Backend) *http.Server {
	h := handlers{backend: b}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static.FS)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /", h.list)
	mux.HandleFunc("GET /images", h.images)
	mux.HandleFunc("GET /instances/new", h.createForm)
	mux.HandleFunc("POST /instances", h.create)
	mux.HandleFunc("GET /instances/{name}", h.detail)
	mux.HandleFunc("GET /instances/{name}/metrics", h.metrics)
	mux.HandleFunc("GET /instances/{name}/export", h.export)
	mux.HandleFunc("POST /instances/{name}/start", h.start)
	mux.HandleFunc("POST /instances/{name}/stop", h.stop)
	mux.HandleFunc("POST /instances/{name}/delete", h.delete)
	mux.HandleFunc("POST /instances/{name}/clone", h.clone)
	mux.HandleFunc("POST /instances/{name}/limits", h.updateLimits)
	mux.HandleFunc("POST /instances/{name}/snapshots", h.createSnapshot)
	mux.HandleFunc("POST /instances/{name}/snapshots/{snap}/restore", h.restoreSnapshot)
	mux.HandleFunc("POST /instances/{name}/snapshots/{snap}/delete", h.deleteSnapshot)

	return &http.Server{Handler: mux}
}
