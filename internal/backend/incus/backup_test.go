package incus

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportInstanceStreamsBackupThenDeletesIt(t *testing.T) {
	srv := &instanceServerStub{
		backupOp:       &operationStub{},
		backupDeleteOp: &operationStub{},
		backupBytes:    []byte("backup-tarball-bytes"),
	}
	b := &incusBackend{srv: srv}

	var buf bytes.Buffer
	require.NoError(t, b.ExportInstance(t.Context(), "demo", &buf))

	assert.Equal(t, "backup-tarball-bytes", buf.String(), "spooled backup should stream to the writer")
	require.NotNil(t, srv.backupRequest.Canceler, "backup download should be cancelable")
	assert.NotEmpty(t, srv.deletedBackup, "the temporary backup should be deleted afterwards")
}

func TestExportInstanceCancelsBackupOperationOnContextCancel(t *testing.T) {
	op := &operationStub{waitErr: context.Canceled}
	srv := &instanceServerStub{backupOp: op, backupDeleteOp: &operationStub{}}
	b := &incusBackend{srv: srv}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	err := b.ExportInstance(ctx, "demo", &buf)

	require.Error(t, err)
	assert.True(t, op.cancelUsed, "a canceled create wait should cancel the server operation")
	assert.NotEmpty(t, srv.deletedBackup, "cleanup should still run after cancellation")
}

func TestExportInstanceStopsSpoolingWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	srv := &instanceServerStub{
		backupOp:          &operationStub{},
		backupDeleteOp:    &operationStub{},
		backupBytes:       []byte("backup-tarball-bytes"),
		backupBeforeWrite: cancel,
	}
	b := &incusBackend{srv: srv}

	var buf bytes.Buffer
	err := b.ExportInstance(ctx, "demo", &buf)

	require.ErrorIs(t, err, context.Canceled)
	require.NotNil(t, srv.backupRequest.Canceler)
	assert.Empty(t, buf.String())
}

func TestImportInstanceCreatesFromBackup(t *testing.T) {
	srv := &instanceServerStub{importOp: &operationStub{}}
	b := &incusBackend{srv: srv}

	require.NoError(t, b.ImportInstance(t.Context(), "restored", strings.NewReader("tarball-bytes")))

	assert.Equal(t, "restored", srv.importedName, "destination name should be passed through")
	assert.Equal(t, "tarball-bytes", string(srv.importedBytes), "the reader should stream to the backup file")
}

func TestImportInstanceStopsReadingWhenContextIsCanceled(t *testing.T) {
	srv := &instanceServerStub{importOp: &operationStub{}}
	b := &incusBackend{srv: srv}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := b.ImportInstance(ctx, "restored", strings.NewReader("tarball-bytes"))

	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, srv.importReadErr, context.Canceled)
	assert.Empty(t, srv.importedBytes)
}

func TestImportInstanceCancelsOperationWhenWaitIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	op := &operationStub{waitErr: context.Canceled, onWait: cancel}
	srv := &instanceServerStub{importOp: op}
	b := &incusBackend{srv: srv}

	err := b.ImportInstance(ctx, "restored", strings.NewReader(""))

	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, op.cancelUsed)
}

func TestCleanupExportTempLogsCloseFailure(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "export-*.tar.gz")
	require.NoError(t, err)
	require.NoError(t, tmp.Close())

	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })

	cleanupExportTemp(tmp)

	assert.Contains(t, logs.String(), tmp.Name())
	assert.Contains(t, logs.String(), "close export temp file")
}

func TestCleanupExportTempLogsRemoveFailure(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "child"), []byte("data"), 0o600))
	tmp, err := os.Open(dir)
	require.NoError(t, err)

	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })

	cleanupExportTemp(tmp)

	assert.Contains(t, logs.String(), dir)
	assert.Contains(t, logs.String(), "remove export temp file")
}
