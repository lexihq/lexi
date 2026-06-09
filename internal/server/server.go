// Package server wires the HTTP routes for the lxcon web UI.
package server

import (
	"log"
	"net/http"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/static"
)

const readHeaderTimeout = 5 * time.Second

// New builds an HTTP server with all lxcon routes registered. The backend is
// injected here so handlers stay driver-agnostic as the UI grows.
func New(b backend.Backend) *http.Server {
	h := handlers{backend: b}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static.FS)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("ok\n")); err != nil {
			log.Printf("lxcon: write health response: %v", err)
		}
	})
	mux.HandleFunc("GET /", h.list)
	mux.HandleFunc("GET /partials/sidebar", h.sidebar)
	mux.HandleFunc("GET /images", h.images)
	mux.HandleFunc("GET /profiles", h.profiles)
	mux.HandleFunc("GET /profiles/{name}", h.profileDetail)
	mux.HandleFunc("GET /instances/new", h.createForm)
	mux.HandleFunc("GET /instances/import", h.importForm)
	mux.HandleFunc("POST /instances/import", h.importInstance)
	mux.HandleFunc("POST /instances", h.create)
	mux.HandleFunc("GET /instances/{name}", h.detail)
	mux.HandleFunc("GET /instances/{name}/metrics", h.metrics)
	mux.HandleFunc("GET /instances/{name}/logs", h.logs)
	mux.HandleFunc("GET /instances/{name}/console", h.console)
	mux.HandleFunc("GET /instances/{name}/console/ws", h.consoleWS)
	mux.HandleFunc("GET /instances/{name}/export", h.export)
	mux.HandleFunc("POST /instances/{name}/start", h.start)
	mux.HandleFunc("POST /instances/{name}/stop", h.stop)
	mux.HandleFunc("POST /instances/{name}/restart", h.restart)
	mux.HandleFunc("POST /instances/{name}/pause", h.pause)
	mux.HandleFunc("POST /instances/{name}/resume", h.resume)
	mux.HandleFunc("POST /instances/{name}/delete", h.delete)
	mux.HandleFunc("POST /instances/{name}/clone", h.clone)
	mux.HandleFunc("POST /instances/{name}/limits", h.updateLimits)
	mux.HandleFunc("POST /instances/{name}/profiles", h.setInstanceProfiles)
	mux.HandleFunc("GET /instances/{name}/config", h.config)
	mux.HandleFunc("POST /instances/{name}/config", h.updateConfig)
	mux.HandleFunc("POST /instances/{name}/snapshots", h.createSnapshot)
	mux.HandleFunc("POST /instances/{name}/snapshots/{snap}/restore", h.restoreSnapshot)
	mux.HandleFunc("POST /instances/{name}/snapshots/{snap}/delete", h.deleteSnapshot)

	return &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}
}
