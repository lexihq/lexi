package incus

import (
	"bytes"
	"testing"
	"time"

	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lexihq/lexi/internal/backend"
)

func TestListVolumeBackupsMapsFields(t *testing.T) {
	created := time.Now().Add(-time.Hour).UTC()
	expires := time.Now().Add(time.Hour).UTC()
	s := &instanceServerStub{volBackups: []api.StorageVolumeBackup{
		{Name: "backup0", CreatedAt: created, ExpiresAt: expires, VolumeOnly: true},
	}}
	b := &incusBackend{srv: s}

	bks, err := b.ListVolumeBackups(t.Context(), "pool", "vol")
	require.NoError(t, err)
	require.Len(t, bks, 1)
	assert.Equal(t, backend.VolumeBackup{Name: "backup0", CreatedAt: created, ExpiresAt: expires, VolumeOnly: true}, bks[0])
}

func TestCreateVolumeBackupDefaultsName(t *testing.T) {
	s := &instanceServerStub{
		volBackups:  []api.StorageVolumeBackup{{Name: "backup0"}},
		volBackupOp: &operationStub{},
	}
	b := &incusBackend{srv: s}

	require.NoError(t, b.CreateVolumeBackup(t.Context(), "pool", "vol", "", time.Time{}, true))
	require.NotNil(t, s.createdVolBackup)
	// backup0 is taken, so the first free index is backup1.
	assert.Equal(t, "backup1", s.createdVolBackup.Name)
	assert.True(t, s.createdVolBackup.VolumeOnly)
	assert.Equal(t, "gzip", s.createdVolBackup.CompressionAlgorithm)
}

func TestCreateVolumeBackupExpiryNeedsExtension(t *testing.T) {
	s := &instanceServerStub{volBackupOp: &operationStub{}} // no backup_expiry extension
	b := &incusBackend{srv: s}

	err := b.CreateVolumeBackup(t.Context(), "pool", "vol", "weekly", time.Now().Add(time.Hour), false)
	require.ErrorIs(t, err, backend.ErrUnsupported)
	assert.Nil(t, s.createdVolBackup, "must not create when expiry is unsupported")
}

func TestCreateVolumeBackupExpiryWithExtension(t *testing.T) {
	s := &instanceServerStub{
		volBackupOp: &operationStub{},
		extensions:  map[string]bool{"backup_expiry": true},
	}
	b := &incusBackend{srv: s}

	require.NoError(t, b.CreateVolumeBackup(t.Context(), "pool", "vol", "weekly", time.Now().Add(time.Hour), false))
	require.NotNil(t, s.createdVolBackup)
	assert.Equal(t, "weekly", s.createdVolBackup.Name)
}

func TestExportVolumeBackupStreamsBytes(t *testing.T) {
	s := &instanceServerStub{volBackupBytes: []byte("tarball-bytes")}
	b := &incusBackend{srv: s}

	var buf bytes.Buffer
	require.NoError(t, b.ExportVolumeBackup(t.Context(), "pool", "vol", "backup0", &buf))
	assert.Equal(t, "tarball-bytes", buf.String())
}

func TestRestoreVolumeBackupTargetsPool(t *testing.T) {
	s := &instanceServerStub{
		volBackupBytes: []byte("tarball-bytes"),
		volImportOp:    &operationStub{},
	}
	b := &incusBackend{srv: s}

	require.NoError(t, b.RestoreVolumeBackup(t.Context(), "pool", "vol", "backup0", "other", "vol2"))
	require.NotNil(t, s.volImportArgs)
	assert.Equal(t, "vol2", s.volImportArgs.Name)
	assert.Equal(t, []byte("tarball-bytes"), s.volImportedBytes)
}

func TestDeleteVolumeBackup(t *testing.T) {
	s := &instanceServerStub{volBackupDeleteOp: &operationStub{}}
	b := &incusBackend{srv: s}

	require.NoError(t, b.DeleteVolumeBackup(t.Context(), "pool", "vol", "backup0"))
	assert.Equal(t, "backup0", s.deletedVolBackup)
}
