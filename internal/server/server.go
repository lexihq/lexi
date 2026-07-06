// Package server wires the HTTP routes for the lexi web UI.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/metrics"
	"github.com/lexihq/lexi/static"
)

const readHeaderTimeout = 5 * time.Second

// metricsHistory is the number of samples retained per instance. At the 3s
// chart cadence this is ~5 minutes of history.
const metricsHistory = 100

// metricsInterval is how often the background sampler polls running instances.
// Keep it in sync with the metrics panel's HTMX "every 3s" refresh
// (instance.templ) and the chart JS POLL_MS (metrics-charts.js) so all three
// cadences line up and the chart spacing stays even.
const metricsInterval = 3 * time.Second

// Option customizes the server built by New.
type Option func(*handlers)

// WithMetricsSampler starts a background goroutine that records per-instance
// metrics into the history store on every metricsInterval tick, so charts have
// data before the metrics tab is opened. The goroutine stops when ctx is done.
func WithMetricsSampler(ctx context.Context) Option {
	return func(h *handlers) {
		go metrics.NewSampler(h.backend, h.samples, metricsInterval).Run(ctx)
	}
}

// New builds an HTTP server with all lexi routes registered. The backend is
// injected here so handlers stay driver-agnostic as the UI grows.
func New(b backend.Backend, opts ...Option) *http.Server {
	h := handlers{backend: b, samples: metrics.NewStore(metricsHistory)}
	for _, opt := range opts {
		opt(&h)
	}
	mux := http.NewServeMux()
	// "/{$}" matches only the root: an unknown path must 404, not render the
	// instances page (and pay its backend calls) for every typo URL.
	mux.HandleFunc("GET /{$}", h.list)
	mux.HandleFunc("GET /partials/sidebar", h.sidebar)
	mux.HandleFunc("GET /partials/instances", h.instancesPartial)
	mux.HandleFunc("POST /project", h.selectProject)
	mux.HandleFunc("POST /remote", h.selectRemote)
	mux.HandleFunc("POST /instances/{name}/migrate", h.migrateInstance)
	mux.HandleFunc("GET /projects", h.projectsPage)
	mux.HandleFunc("POST /projects", h.createProject)
	mux.HandleFunc("GET /projects/{name}", h.projectDetail)
	mux.HandleFunc("POST /projects/{name}/config", h.updateProject)
	mux.HandleFunc("POST /projects/{name}/limits", h.updateProjectLimits)
	mux.HandleFunc("POST /projects/{name}/rename", h.renameProject)
	mux.HandleFunc("POST /projects/{name}/delete", h.deleteProject)
	mux.HandleFunc("GET /partials/images", h.imagePicker)
	mux.HandleFunc("GET /partials/operations", h.operationsPanel)
	mux.HandleFunc("GET /events/operations", h.operationsEvents)
	mux.HandleFunc("POST /operations/{id}/cancel", h.cancelOperation)
	mux.HandleFunc("GET /images", h.imagesPage)
	mux.HandleFunc("POST /images/copy", h.copyImage)
	mux.HandleFunc("POST /images/publish", h.publishImage)
	mux.HandleFunc("POST /images/import", h.importImage)
	mux.HandleFunc("GET /images/{fingerprint}/export", h.exportImage)
	mux.HandleFunc("POST /images/{fingerprint}/config", h.updateImage)
	mux.HandleFunc("POST /images/{fingerprint}/delete", h.deleteImage)
	mux.HandleFunc("POST /images/{fingerprint}/refresh", h.refreshImage)
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
	mux.HandleFunc("POST /networks/{name}/forwards", h.createNetworkForward)
	mux.HandleFunc("POST /networks/{name}/forwards/{addr}/update", h.updateNetworkForward)
	mux.HandleFunc("POST /networks/{name}/forwards/{addr}/delete", h.deleteNetworkForward)
	mux.HandleFunc("POST /networks/{name}/config", h.updateNetwork)
	mux.HandleFunc("POST /networks/{name}/delete", h.deleteNetwork)
	mux.HandleFunc("GET /network-acls", h.networkACLs)
	mux.HandleFunc("POST /network-acls", h.createNetworkACL)
	mux.HandleFunc("GET /network-acls/{name}", h.networkACLDetail)
	mux.HandleFunc("POST /network-acls/{name}/config", h.updateNetworkACL)
	mux.HandleFunc("POST /network-acls/{name}/rename", h.renameNetworkACL)
	mux.HandleFunc("POST /network-acls/{name}/delete", h.deleteNetworkACL)
	mux.HandleFunc("POST /network-acls/{name}/rules", h.addNetworkACLRule)
	mux.HandleFunc("POST /network-acls/{name}/rules/delete", h.deleteNetworkACLRule)
	mux.HandleFunc("GET /network-zones", h.networkZones)
	mux.HandleFunc("POST /network-zones", h.createNetworkZone)
	mux.HandleFunc("GET /network-zones/{name}", h.networkZoneDetail)
	mux.HandleFunc("POST /network-zones/{name}/config", h.updateNetworkZone)
	mux.HandleFunc("POST /network-zones/{name}/delete", h.deleteNetworkZone)
	mux.HandleFunc("POST /network-zones/{name}/records", h.addZoneRecord)
	mux.HandleFunc("POST /network-zones/{name}/records/delete", h.deleteZoneRecord)
	mux.HandleFunc("GET /server", h.serverPage)
	mux.HandleFunc("POST /server/config", h.updateServerConfig)
	mux.HandleFunc("POST /server/certificates", h.addCertificate)
	mux.HandleFunc("POST /server/certificates/{fingerprint}/delete", h.deleteCertificate)
	mux.HandleFunc("POST /server/certificates/{fingerprint}/update", h.updateCertificate)
	mux.HandleFunc("POST /server/warnings/{uuid}/delete", h.deleteWarning)
	mux.HandleFunc("POST /server/warnings/{uuid}/ack", h.ackWarning)
	mux.HandleFunc("GET /storage", h.storagePools)
	mux.HandleFunc("GET /storage/new", h.poolCreateForm)
	mux.HandleFunc("POST /storage", h.createPool)
	mux.HandleFunc("GET /storage/{pool}", h.storagePool)
	mux.HandleFunc("POST /storage/{pool}/config", h.updatePool)
	mux.HandleFunc("POST /storage/{pool}/delete", h.deletePool)
	mux.HandleFunc("POST /storage/{pool}/buckets", h.createBucket)
	mux.HandleFunc("POST /storage/{pool}/buckets/{bucket}/delete", h.deleteBucket)
	mux.HandleFunc("POST /storage/{pool}/buckets/{bucket}/keys", h.createBucketKey)
	mux.HandleFunc("POST /storage/{pool}/buckets/{bucket}/keys/delete", h.deleteBucketKey)
	mux.HandleFunc("POST /storage/{pool}/volumes", h.createVolume)
	mux.HandleFunc("GET /storage/{pool}/volumes/{volume}", h.storageVolume)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/delete", h.deleteVolume)
	mux.HandleFunc("GET /storage/{pool}/volumes/{volume}/export", h.exportVolume)
	mux.HandleFunc("POST /storage/{pool}/volumes/import", h.importVolume)
	mux.HandleFunc("POST /storage/{pool}/volumes/iso", h.uploadISOVolume)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/config", h.updateVolume)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/rename", h.renameVolume)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/snapshots", h.createVolumeSnapshot)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/snapshots/{snap}/restore", h.restoreVolumeSnapshot)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/snapshots/{snap}/rename", h.renameVolumeSnapshot)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/snapshots/{snap}/expiry", h.updateVolumeSnapshotExpiry)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/snapshots/{snap}/delete", h.deleteVolumeSnapshot)
	mux.HandleFunc("GET /storage/{pool}/volumes/{volume}/backups", h.volumeBackupsPanel)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/backups", h.createVolumeBackup)
	mux.HandleFunc("GET /storage/{pool}/volumes/{volume}/backups/{backup}/export", h.exportVolumeBackup)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/backups/{backup}/restore", h.restoreVolumeBackup)
	mux.HandleFunc("POST /storage/{pool}/volumes/{volume}/backups/{backup}/delete", h.deleteVolumeBackup)
	mux.HandleFunc("GET /instances/new", h.createForm)
	mux.HandleFunc("GET /instances/import", h.importForm)
	mux.HandleFunc("POST /instances/import", h.importInstance)
	mux.HandleFunc("POST /instances/bulk", h.bulk)
	mux.HandleFunc("POST /instances", h.create)
	mux.HandleFunc("GET /instances/{name}", h.detail)
	mux.HandleFunc("GET /instances/{name}/rebuild", h.rebuildForm)
	mux.HandleFunc("POST /instances/{name}/rebuild", h.rebuild)
	mux.HandleFunc("GET /instances/{name}/metrics", h.metrics)
	mux.HandleFunc("GET /instances/{name}/metrics/series", h.metricsSeries)
	mux.HandleFunc("GET /instances/{name}/logs", h.logs)
	mux.HandleFunc("GET /instances/{name}/backups", h.backupsPanel)
	mux.HandleFunc("POST /instances/{name}/backups", h.createStoredBackup)
	mux.HandleFunc("GET /instances/{name}/backups/{backup}/download", h.downloadStoredBackup)
	mux.HandleFunc("POST /instances/{name}/backups/{backup}/restore", h.restoreStoredBackup)
	mux.HandleFunc("POST /instances/{name}/backups/{backup}/delete", h.deleteStoredBackup)
	mux.HandleFunc("GET /instances/{name}/console", h.console)
	mux.HandleFunc("GET /instances/{name}/console/ws", h.consoleWS)
	mux.HandleFunc("GET /instances/{name}/export", h.export)
	mux.HandleFunc("POST /instances/{name}/start", h.start)
	mux.HandleFunc("POST /instances/{name}/stop", h.stop)
	mux.HandleFunc("POST /instances/{name}/restart", h.restart)
	mux.HandleFunc("POST /instances/{name}/pause", h.pause)
	mux.HandleFunc("POST /instances/{name}/resume", h.resume)
	mux.HandleFunc("POST /instances/{name}/rescue", h.rescue)
	mux.HandleFunc("POST /instances/{name}/delete", h.delete)
	mux.HandleFunc("POST /instances/{name}/clone", h.clone)
	mux.HandleFunc("POST /instances/{name}/rename", h.renameInstance)
	mux.HandleFunc("POST /instances/{name}/move", h.moveInstance)
	mux.HandleFunc("POST /instances/{name}/limits", h.updateLimits)
	mux.HandleFunc("POST /instances/{name}/profiles", h.setInstanceProfiles)
	mux.HandleFunc("GET /instances/{name}/config", h.config)
	mux.HandleFunc("POST /instances/{name}/config", h.updateConfig)
	mux.HandleFunc("POST /instances/{name}/options", h.updateOptions)
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

	// Static assets and the health probe bypass the remote/project middleware:
	// they never touch the backend, and routing them through it would cost a
	// ListRemotes/GetProject daemon round-trip per asset request — and let a
	// stale cookie fail /healthz while the process is healthy.
	outer := http.NewServeMux()
	staticFiles := http.StripPrefix("/static/", http.FileServerFS(static.FS))
	outer.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Embedded assets change only on redeploy; embed.FS has no ModTime, so
		// without a cache header every page load re-downloads all of them.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		staticFiles.ServeHTTP(w, r)
	}))
	outer.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("ok\n")); err != nil {
			slog.Warn("write health response", "err", err)
		}
	})
	outer.Handle("/", csrfGuard(h.withRemote(h.withProject(mux))))

	return &http.Server{
		Handler:           outer,
		ReadHeaderTimeout: readHeaderTimeout,
	}
}
