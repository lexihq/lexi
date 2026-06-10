// Package server wires the HTTP routes for the lxcon web UI.
package server

import (
	"log/slog"
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
			slog.Warn("write health response", "err", err)
		}
	})
	mux.HandleFunc("GET /", h.list)
	mux.HandleFunc("GET /partials/sidebar", h.sidebar)
	mux.HandleFunc("GET /partials/images", h.imagePicker)
	mux.HandleFunc("GET /partials/operations", h.operationsPanel)
	mux.HandleFunc("POST /operations/{id}/cancel", h.cancelOperation)
	mux.HandleFunc("GET /images", h.imagesPage)
	mux.HandleFunc("POST /images/copy", h.copyImage)
	mux.HandleFunc("POST /images/publish", h.publishImage)
	mux.HandleFunc("POST /images/import", h.importImage)
	mux.HandleFunc("GET /images/{fingerprint}/export", h.exportImage)
	mux.HandleFunc("POST /images/{fingerprint}/config", h.updateImage)
	mux.HandleFunc("POST /images/{fingerprint}/delete", h.deleteImage)
	mux.HandleFunc("POST /images/{fingerprint}/aliases", h.addImageAlias)
	mux.HandleFunc("POST /images/aliases/delete", h.removeImageAlias)
	mux.HandleFunc("GET /profiles", h.profiles)
	mux.HandleFunc("POST /profiles", h.createProfile)
	mux.HandleFunc("GET /profiles/{name}", h.profileDetail)
	mux.HandleFunc("POST /profiles/{name}/config", h.updateProfile)
	mux.HandleFunc("POST /profiles/{name}/rename", h.renameProfile)
	mux.HandleFunc("POST /profiles/{name}/delete", h.deleteProfile)
	mux.HandleFunc("POST /profiles/{name}/devices", h.addProfileDevice)
	mux.HandleFunc("POST /profiles/{name}/devices/{device}", h.updateProfileDevice)
	mux.HandleFunc("POST /profiles/{name}/devices/{device}/delete", h.removeProfileDevice)
	mux.HandleFunc("GET /networks", h.networks)
	mux.HandleFunc("GET /networks/new", h.networkCreateForm)
	mux.HandleFunc("POST /networks", h.createNetwork)
	mux.HandleFunc("GET /networks/{name}", h.networkDetail)
	mux.HandleFunc("POST /networks/{name}/config", h.updateNetwork)
	mux.HandleFunc("POST /networks/{name}/delete", h.deleteNetwork)
	mux.HandleFunc("GET /server", h.serverPage)
	mux.HandleFunc("POST /server/config", h.updateServerConfig)
	mux.HandleFunc("POST /server/certificates", h.addCertificate)
	mux.HandleFunc("POST /server/certificates/{fingerprint}/delete", h.deleteCertificate)
	mux.HandleFunc("POST /server/warnings/{uuid}/delete", h.deleteWarning)
	mux.HandleFunc("POST /server/warnings/{uuid}/ack", h.ackWarning)
	mux.HandleFunc("GET /storage", h.storagePools)
	mux.HandleFunc("GET /storage/new", h.poolCreateForm)
	mux.HandleFunc("POST /storage", h.createPool)
	mux.HandleFunc("GET /storage/{pool}", h.storagePool)
	mux.HandleFunc("POST /storage/{pool}/config", h.updatePool)
	mux.HandleFunc("POST /storage/{pool}/delete", h.deletePool)
	mux.HandleFunc("POST /storage/{pool}/volumes", h.createVolume)
	mux.HandleFunc("GET /storage/{pool}/volumes/{volume}", h.storageVolume)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/delete", h.deleteVolume)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/config", h.updateVolume)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/rename", h.renameVolume)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/snapshots", h.createVolumeSnapshot)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/snapshots/{snap}/restore", h.restoreVolumeSnapshot)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/snapshots/{snap}/rename", h.renameVolumeSnapshot)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/snapshots/{snap}/expiry", h.updateVolumeSnapshotExpiry)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/snapshots/{snap}/delete", h.deleteVolumeSnapshot)
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
	mux.HandleFunc("POST /instances/{name}/rename", h.renameInstance)
	mux.HandleFunc("POST /instances/{name}/move", h.moveInstance)
	mux.HandleFunc("POST /instances/{name}/limits", h.updateLimits)
	mux.HandleFunc("POST /instances/{name}/profiles", h.setInstanceProfiles)
	mux.HandleFunc("GET /instances/{name}/config", h.config)
	mux.HandleFunc("POST /instances/{name}/config", h.updateConfig)
	mux.HandleFunc("GET /instances/{name}/devices", h.devicesPanel)
	mux.HandleFunc("GET /instances/{name}/files", h.filesPanel)
	mux.HandleFunc("GET /instances/{name}/files/download", h.downloadFile)
	mux.HandleFunc("POST /instances/{name}/files/upload", h.uploadFile)
	mux.HandleFunc("POST /instances/{name}/files/delete", h.deleteFile)
	mux.HandleFunc("POST /instances/{name}/files/mkdir", h.makeDirectory)
	mux.HandleFunc("GET /instances/{name}/files/view", h.viewFile)
	mux.HandleFunc("GET /instances/{name}/files/edit", h.editFileForm)
	mux.HandleFunc("POST /instances/{name}/files/edit", h.saveFile)
	mux.HandleFunc("POST /instances/{name}/devices", h.addDevice)
	mux.HandleFunc("POST /instances/{name}/devices/{device}", h.updateDevice)
	mux.HandleFunc("POST /instances/{name}/devices/{device}/delete", h.removeDevice)
	mux.HandleFunc("POST /instances/{name}/snapshots", h.createSnapshot)
	mux.HandleFunc("GET /instances/{name}/snapshots/schedule", h.snapshotSchedule)
	mux.HandleFunc("POST /instances/{name}/snapshots/schedule", h.setSnapshotSchedule)
	mux.HandleFunc("POST /instances/{name}/snapshots/{snap}/rename", h.renameSnapshot)
	mux.HandleFunc("POST /instances/{name}/snapshots/{snap}/expiry", h.updateSnapshotExpiry)
	mux.HandleFunc("POST /instances/{name}/snapshots/{snap}/restore", h.restoreSnapshot)
	mux.HandleFunc("POST /instances/{name}/snapshots/{snap}/delete", h.deleteSnapshot)

	return &http.Server{
		Handler:           csrfGuard(mux),
		ReadHeaderTimeout: readHeaderTimeout,
	}
}
