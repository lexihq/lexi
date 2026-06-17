package incus

import (
	"context"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetServerOverviewMapsEnvironmentAndResources(t *testing.T) {
	b := &incusBackend{srv: &instanceServerStub{
		server: &api.Server{
			ServerUntrusted: api.ServerUntrusted{},
			Environment: api.ServerEnvironment{
				ServerVersion: "6.23",
				Kernel:        "Linux",
				KernelVersion: "6.8.0",
				Driver:        "lxc | qemu",
				DriverVersion: "6.0.4 | 9.0.2",
			},
		},
		resources: &api.Resources{
			CPU:    api.ResourcesCPU{Total: 16},
			Memory: api.ResourcesMemory{Used: 1 << 30, Total: 8 << 30},
		},
	}}

	got, err := b.GetServerOverview(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "6.23", got.ServerVersion)
	assert.Equal(t, "Linux", got.Kernel)
	assert.Equal(t, "6.8.0", got.KernelVersion)
	assert.Equal(t, "lxc | qemu", got.Driver)
	assert.Equal(t, "6.0.4 | 9.0.2", got.DriverVersion)
	assert.Equal(t, 16, got.CPUThreads)
	assert.Equal(t, int64(1<<30), got.MemoryUsed)
	assert.Equal(t, int64(8<<30), got.MemoryTotal)
}

func TestGetServerHardwareMapsResourceTopology(t *testing.T) {
	b := &incusBackend{srv: &instanceServerStub{
		resources: &api.Resources{
			GPU: api.ResourcesGPU{Cards: []api.ResourcesGPUCard{{
				Vendor: "Intel Corporation", Product: "HD Graphics 620", Driver: "i915", PCIAddress: "0000:00:02.0",
			}}},
			Network: api.ResourcesNetwork{Cards: []api.ResourcesNetworkCard{{
				Vendor: "Aquantia Corp.", Product: "AQC107 NBase-T", Driver: "atlantic", PCIAddress: "0000:0d:00.0",
				Ports: []api.ResourcesNetworkCardPort{{ID: "eth0", Address: "00:23:a4:01:01:6f"}},
			}}},
			Storage: api.ResourcesStorage{Disks: []api.ResourcesStorageDisk{{
				ID: "nvme0n1", Model: "INTEL SSDPEKKW256G7", Type: "nvme", Size: 256 << 30, Removable: true,
			}}},
		},
	}}

	got, err := b.GetServerHardware(context.Background())

	require.NoError(t, err)
	require.Len(t, got.GPUs, 1)
	assert.Equal(t, backend.GPUCard{
		Vendor: "Intel Corporation", Product: "HD Graphics 620", Driver: "i915", PCIAddress: "0000:00:02.0",
	}, got.GPUs[0])
	require.Len(t, got.NICs, 1)
	assert.Equal(t, backend.NetworkCard{
		Vendor: "Aquantia Corp.", Product: "AQC107 NBase-T", Driver: "atlantic", PCIAddress: "0000:0d:00.0",
		Ports: []backend.NetworkPort{{ID: "eth0", Address: "00:23:a4:01:01:6f"}},
	}, got.NICs[0])
	require.Len(t, got.Disks, 1)
	assert.Equal(t, backend.HostDisk{
		ID: "nvme0n1", Model: "INTEL SSDPEKKW256G7", Type: "nvme", SizeBytes: 256 << 30, Removable: true,
	}, got.Disks[0])
}

func TestServerConfigGetAndReplace(t *testing.T) {
	srv := &instanceServerStub{
		server: &api.Server{
			ServerUntrusted: api.ServerUntrusted{
				ServerPut: api.ServerPut{Config: api.ConfigMap{"core.https_address": ":8443"}},
			},
		},
	}
	b := &incusBackend{srv: srv}

	cfg, version, err := b.GetServerConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"core.https_address": ":8443"}, cfg)
	assert.Equal(t, "server-etag", version, "version must carry the server etag")

	require.NoError(t, b.UpdateServerConfig(context.Background(), map[string]string{"user.x": "1"}, version))
	require.NotNil(t, srv.updatedServer)
	// Dropped keys must be sent as explicit empty values — the daemon does not
	// treat omission as removal.
	assert.Equal(t, api.ConfigMap{"user.x": "1", "core.https_address": ""}, srv.updatedServer.Config)
	assert.Equal(t, "server-etag", srv.updatedServerEtag, "update must send the caller's etag, not a fresh one")
}

func TestListCertificatesMaps(t *testing.T) {
	b := &incusBackend{srv: &instanceServerStub{certificates: []api.Certificate{{
		CertificatePut: api.CertificatePut{Name: "laptop", Type: "client", Restricted: true},
		Fingerprint:    "abc123",
	}}}}

	got, err := b.ListCertificates(context.Background())

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "laptop", got[0].Name)
	assert.Equal(t, "client", got[0].Type)
	assert.Equal(t, "abc123", got[0].Fingerprint)
	assert.True(t, got[0].Restricted)
}

func TestListCertificatesMapsProjects(t *testing.T) {
	b := &incusBackend{srv: &instanceServerStub{certificates: []api.Certificate{{
		CertificatePut: api.CertificatePut{Name: "ci", Type: "client", Restricted: true, Projects: []string{"default", "dev"}},
		Fingerprint:    "abc123",
	}}}}

	got, err := b.ListCertificates(context.Background())

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"default", "dev"}, got[0].Projects)
}

func TestUpdateCertificateSendsEtagAndKeepsTypeAndCert(t *testing.T) {
	srv := &instanceServerStub{certificate: &api.Certificate{
		CertificatePut: api.CertificatePut{Name: "old", Type: "metrics", Certificate: "base64der"},
		Fingerprint:    "abc123",
	}, certificateEtag: "cert-etag"}
	b := &incusBackend{srv: srv}

	require.NoError(t, b.UpdateCertificate(context.Background(), "abc123", "renamed", true, []string{"dev"}))

	require.NotNil(t, srv.updatedCert)
	// Type and the certificate body must survive the read-modify-write; only
	// name/restriction change.
	assert.Equal(t, "renamed", srv.updatedCert.Name)
	assert.Equal(t, "metrics", srv.updatedCert.Type)
	assert.Equal(t, "base64der", srv.updatedCert.Certificate)
	assert.True(t, srv.updatedCert.Restricted)
	assert.Equal(t, []string{"dev"}, srv.updatedCert.Projects)
	assert.Equal(t, "cert-etag", srv.updatedCertEtag, "update must send the read etag")
}

func TestUpdateCertificateUnrestrictClearsProjects(t *testing.T) {
	srv := &instanceServerStub{certificate: &api.Certificate{
		CertificatePut: api.CertificatePut{Name: "ci", Type: "client", Restricted: true, Projects: []string{"dev"}},
		Fingerprint:    "abc123",
	}}
	b := &incusBackend{srv: srv}

	require.NoError(t, b.UpdateCertificate(context.Background(), "abc123", "ci", false, []string{"dev"}))

	require.NotNil(t, srv.updatedCert)
	assert.False(t, srv.updatedCert.Restricted)
	assert.Empty(t, srv.updatedCert.Projects)
}

func TestUpdateCertificateEmptyNameIsInvalid(t *testing.T) {
	b := &incusBackend{srv: &instanceServerStub{}}
	err := b.UpdateCertificate(context.Background(), "abc123", "", false, nil)
	require.ErrorIs(t, err, backend.ErrInvalid)
}

func TestListWarningsMapsAndSortsNewestFirst(t *testing.T) {
	older := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	b := &incusBackend{srv: &instanceServerStub{warnings: []api.Warning{
		{WarningPut: api.WarningPut{Status: "new"}, UUID: "w-old", Type: "t1", Severity: "low", Count: 2, LastMessage: "m1", LastSeenAt: older},
		{WarningPut: api.WarningPut{Status: "acknowledged"}, UUID: "w-new", Type: "t2", Severity: "high", Count: 1, LastMessage: "m2", LastSeenAt: newer},
	}}}

	got, err := b.ListWarnings(context.Background())

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "w-new", got[0].UUID)
	assert.Equal(t, "high", got[0].Severity)
	assert.Equal(t, "acknowledged", got[0].Status)
	assert.Equal(t, "m2", got[0].LastMessage)
	assert.Equal(t, newer, got[0].LastSeenAt)
	assert.Equal(t, "w-old", got[1].UUID)
}

func TestUpdateServerConfigEtagRaceIsConflict(t *testing.T) {
	srv := &instanceServerStub{server: &api.Server{}}
	b := &incusBackend{srv: srv}
	require.NoError(t, b.UpdateServerConfig(context.Background(), nil, ""))

	// A stale etag races another writer and gets 412 → conflict.
	srv.serverErr = api.StatusErrorf(412, "Precondition failed")
	err := b.UpdateServerConfig(context.Background(), map[string]string{"user.x": "1"}, "stale-etag")
	require.ErrorIs(t, err, backend.ErrConflict)
}

func TestDeleteWarningPassesUUID(t *testing.T) {
	srv := &instanceServerStub{}
	b := &incusBackend{srv: srv}

	require.NoError(t, b.DeleteWarning(context.Background(), "w-1"))

	assert.Equal(t, "w-1", srv.deletedWarning)
}
