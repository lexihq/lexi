package incus

import (
	"context"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
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
