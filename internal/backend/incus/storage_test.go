package incus

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"strings"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToPool(t *testing.T) {
	p := &api.StoragePool{Name: "default", Driver: "dir", UsedBy: []string{"/1.0/instances/c1"}}
	p.Description = "Default pool"
	p.Config = map[string]string{"source": "/var/lib/incus"}
	got := toPool(p)
	assert.Equal(t, "default", got.Name)
	assert.Equal(t, "dir", got.Driver)
	assert.Equal(t, "Default pool", got.Description)
	assert.Equal(t, "/var/lib/incus", got.Config["source"])
	assert.Equal(t, []string{"/1.0/instances/c1"}, got.UsedBy)
}

func TestToVolume(t *testing.T) {
	v := &api.StorageVolume{Name: "vol1", Type: "custom", ContentType: "filesystem", UsedBy: []string{"/1.0/instances/c1"}}
	v.Config = map[string]string{"size": "1GiB"}
	got := toVolume("default", v)
	assert.Equal(t, "vol1", got.Name)
	assert.Equal(t, "custom", got.Type)
	assert.Equal(t, "filesystem", got.ContentType)
	assert.Equal(t, "default", got.Pool)
	assert.Equal(t, "1GiB", got.Config["size"])
	assert.Equal(t, []string{"/1.0/instances/c1"}, got.UsedBy)
}

func TestListVolumesFiltersCustom(t *testing.T) {
	b := &incusBackend{srv: &instanceServerStub{volumes: []api.StorageVolume{
		{Name: "vol1", Type: "custom"},
		{Name: "c1", Type: "container"},
	}}}
	vols, err := b.ListVolumes(t.Context(), "default")
	require.NoError(t, err)
	require.Len(t, vols, 1)
	assert.Equal(t, "vol1", vols[0].Name)
	assert.Equal(t, "default", vols[0].Pool)
}

func TestCreateVolumeSendsPost(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	err := b.CreateVolume(t.Context(), "default", backend.StorageVolume{
		Name: "vol1", ContentType: "filesystem", Config: map[string]string{"size": "1GiB"},
	})
	require.NoError(t, err)
	require.NotNil(t, s.createdVol)
	assert.Equal(t, "vol1", s.createdVol.Name)
	assert.Equal(t, "custom", s.createdVol.Type)
	assert.Equal(t, "filesystem", s.createdVol.ContentType)
	assert.Equal(t, "1GiB", s.createdVol.Config["size"])
}

func TestDeleteVolumeCallsThrough(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.DeleteVolume(t.Context(), "default", "vol1"))
	assert.Equal(t, [3]string{"default", "custom", "vol1"}, s.deletedVol)
}

func TestToVolumeSnapshot(t *testing.T) {
	// Incus reports the name as "<volume>/<snapshot>"; the mapper returns the bare
	// snapshot name.
	s := &api.StorageVolumeSnapshot{Name: "vol1/snap0"}
	got := toVolumeSnapshot(s)
	assert.Equal(t, "snap0", got.Name)
}

func TestCreateVolumeSnapshotWaits(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.CreateVolumeSnapshot(t.Context(), "default", "vol1", "snap0"))
	assert.Equal(t, "snap0", s.createdSnap)
}

func TestRestoreVolumeSnapshotSetsRestorePut(t *testing.T) {
	s := &instanceServerStub{volume: &api.StorageVolume{Name: "vol1", Type: "custom"}}
	b := &incusBackend{srv: s}
	require.NoError(t, b.RestoreVolumeSnapshot(t.Context(), "default", "vol1", "snap0"))
	require.NotNil(t, s.restoredVol)
	assert.Equal(t, "snap0", s.restoredVol.Restore)
}

func TestDeleteVolumeSnapshotCallsThrough(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.DeleteVolumeSnapshot(t.Context(), "default", "vol1", "snap0"))
	assert.Equal(t, "snap0", s.deletedSnap)
}

func TestExportVolumeStreamsBackupThenDeletesIt(t *testing.T) {
	s := &instanceServerStub{
		volBackupOp:       &operationStub{},
		volBackupDeleteOp: &operationStub{},
		volBackupBytes:    []byte("volume-tarball-bytes"),
	}
	b := &incusBackend{srv: s}

	var buf bytes.Buffer
	require.NoError(t, b.ExportVolume(t.Context(), "default", "vol1", &buf))

	assert.Equal(t, "volume-tarball-bytes", buf.String(), "spooled backup should stream to the writer")
	require.NotNil(t, s.createdVolBackup)
	assert.Equal(t, "gzip", s.createdVolBackup.CompressionAlgorithm)
	require.NotNil(t, s.volBackupRequest.Canceler, "backup download should be cancelable")
	assert.Equal(t, s.createdVolBackup.Name, s.deletedVolBackup, "the temporary backup should be deleted afterwards")
}

func TestImportVolumeCreatesFromBackup(t *testing.T) {
	s := &instanceServerStub{volImportOp: &operationStub{}}
	b := &incusBackend{srv: s}

	require.NoError(t, b.ImportVolume(t.Context(), "default", "restored", strings.NewReader("tarball-bytes")))

	require.NotNil(t, s.volImportArgs)
	assert.Equal(t, "restored", s.volImportArgs.Name, "destination volume name should be passed through")
	assert.Equal(t, "tarball-bytes", string(s.volImportedBytes), "the reader should stream to the backup file")
}

func TestImportVolumeInvalidNameRejected(t *testing.T) {
	b := &incusBackend{srv: &instanceServerStub{}}
	err := b.ImportVolume(t.Context(), "default", "bad name", strings.NewReader("x"))
	require.ErrorIs(t, err, backend.ErrInvalid)
}

// instanceBackupTarball builds the head of an instance backup: a gzip tarball
// whose first entry is backup/index.yaml with an instance type.
func instanceBackupTarball(t *testing.T, backupType string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	yaml := "name: c1\ntype: " + backupType + "\n"
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "backup/index.yaml", Mode: 0o644, Size: int64(len(yaml))}))
	_, err := tw.Write([]byte(yaml))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func TestImportVolumeRejectsInstanceBackup(t *testing.T) {
	s := &instanceServerStub{volImportOp: &operationStub{}}
	b := &incusBackend{srv: s}

	for _, typ := range []string{"container", "virtual-machine"} {
		err := b.ImportVolume(t.Context(), "default", "v1", bytes.NewReader(instanceBackupTarball(t, typ)))
		require.ErrorIs(t, err, backend.ErrInvalid, typ)
	}
	assert.Nil(t, s.volImportArgs, "instance backups must be rejected before any upload")

	// A custom-volume backup with the same shape passes the type check.
	require.NoError(t, b.ImportVolume(t.Context(), "default", "v1", bytes.NewReader(instanceBackupTarball(t, "custom"))))
	require.NotNil(t, s.volImportArgs)
}

func TestCreateVolumeFromISOStreamsUpload(t *testing.T) {
	s := &instanceServerStub{volISOOp: &operationStub{}}
	b := &incusBackend{srv: s}

	require.NoError(t, b.CreateVolumeFromISO(t.Context(), "default", "install-media", strings.NewReader("iso-bytes")))

	require.NotNil(t, s.volISOArgs)
	assert.Equal(t, "install-media", s.volISOArgs.Name, "volume name should be passed through")
	assert.Equal(t, "iso-bytes", string(s.volISOBytes), "the reader should stream to the upload")
}

func TestCreateVolumeFromISOInvalidNameRejected(t *testing.T) {
	s := &instanceServerStub{}
	err := (&incusBackend{srv: s}).CreateVolumeFromISO(t.Context(), "default", "bad name", strings.NewReader("x"))
	require.ErrorIs(t, err, backend.ErrInvalid)
	assert.Nil(t, s.volISOArgs, "invalid names must be rejected before any upload")
}

func TestImportVolumeUniqueConstraintRaceIsConflict(t *testing.T) {
	// Two concurrent imports of the same new name: the loser fails on the
	// daemon's DB unique constraint, not the "already exists" pre-check.
	s := &instanceServerStub{volImportOp: &operationStub{
		waitErr: errors.New(`Error inserting volume "v1" for project "default" in pool "default" of type "custom" into database "UNIQUE constraint failed: storage_volumes.storage_pool_id, storage_volumes.node_id, storage_volumes.project_id, storage_volumes.name, storage_volumes.type"`),
	}}
	b := &incusBackend{srv: s}
	err := b.ImportVolume(t.Context(), "default", "v1", strings.NewReader("x"))
	require.ErrorIs(t, err, backend.ErrConflict)
}
