package incus

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"

	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type instanceServerStub struct {
	incusclient.InstanceServer
	snapshotErr       error
	listType          api.InstanceType
	state             *api.InstanceState
	instance          *api.Instance
	deleteOp          incusclient.Operation
	copyOp            incusclient.RemoteOperation
	backupOp          incusclient.Operation
	backupDeleteOp    incusclient.Operation
	backupBytes       []byte
	backupRequest     *incusclient.BackupFileRequest
	backupBeforeWrite func()
	deletedBackup     string
	importOp          incusclient.Operation
	importedName      string
	importedBytes     []byte
	importReadErr     error
	consoleLog        string
	consoleErr        error
	consoleCloseErr   error
	stateAction       string                // last UpdateInstanceState action
	stateOp           incusclient.Operation // operation returned by UpdateInstanceState
}

func (s *instanceServerStub) GetInstanceSnapshots(string) ([]api.InstanceSnapshot, error) {
	return nil, s.snapshotErr
}

func (s *instanceServerStub) GetInstancesFull(instanceType api.InstanceType) ([]api.InstanceFull, error) {
	s.listType = instanceType
	return nil, nil
}

func (s *instanceServerStub) GetInstanceState(string) (*api.InstanceState, string, error) {
	return s.state, "", nil
}

func (s *instanceServerStub) DeleteInstance(string) (incusclient.Operation, error) {
	return s.deleteOp, nil
}

func (s *instanceServerStub) GetInstance(string) (*api.Instance, string, error) {
	return s.instance, "", nil
}

func (s *instanceServerStub) CopyInstance(incusclient.InstanceServer, api.Instance, *incusclient.InstanceCopyArgs) (incusclient.RemoteOperation, error) {
	return s.copyOp, nil
}

func (s *instanceServerStub) CreateInstanceBackup(string, api.InstanceBackupsPost) (incusclient.Operation, error) {
	return s.backupOp, nil
}

func (s *instanceServerStub) GetInstanceBackupFile(_ string, name string, req *incusclient.BackupFileRequest) (*incusclient.BackupFileResponse, error) {
	s.backupRequest = req
	if s.backupBeforeWrite != nil {
		s.backupBeforeWrite()
	}
	if _, err := req.BackupFile.Write(s.backupBytes); err != nil {
		return nil, err
	}
	return &incusclient.BackupFileResponse{Size: int64(len(s.backupBytes))}, nil
}

func (s *instanceServerStub) DeleteInstanceBackup(_ string, name string) (incusclient.Operation, error) {
	s.deletedBackup = name
	return s.backupDeleteOp, nil
}

func (s *instanceServerStub) CreateInstanceFromBackup(args incusclient.InstanceBackupArgs) (incusclient.Operation, error) {
	s.importedName = args.Name
	s.importedBytes, s.importReadErr = io.ReadAll(args.BackupFile)
	if s.importReadErr != nil {
		return nil, s.importReadErr
	}
	return s.importOp, nil
}

func (s *instanceServerStub) GetInstanceConsoleLog(string, *incusclient.InstanceConsoleLogArgs) (io.ReadCloser, error) {
	if s.consoleErr != nil {
		return nil, s.consoleErr
	}
	return &readCloserStub{
		Reader:   strings.NewReader(s.consoleLog),
		closeErr: s.consoleCloseErr,
	}, nil
}

type readCloserStub struct {
	io.Reader
	closeErr error
}

func (r *readCloserStub) Close() error {
	return r.closeErr
}

func (s *instanceServerStub) UpdateInstanceState(_ string, req api.InstanceStatePut, _ string) (incusclient.Operation, error) {
	s.stateAction = req.Action
	if s.stateOp != nil {
		return s.stateOp, nil
	}
	return &operationStub{}, nil
}

type operationStub struct {
	incusclient.Operation
	waitErr         error
	waitContextUsed bool
	cancelUsed      bool
	onWait          func()
}

func (o *operationStub) WaitContext(context.Context) error {
	o.waitContextUsed = true
	if o.onWait != nil {
		o.onWait()
	}
	return o.waitErr
}

func (o *operationStub) Cancel() error {
	o.cancelUsed = true
	return nil
}

type remoteOperationStub struct {
	incusclient.RemoteOperation
	waitErr    error
	cancelErr  error
	started    chan struct{}
	cancelled  chan struct{}
	cancelUsed bool
}

func (o *remoteOperationStub) Wait() error {
	close(o.started)
	<-o.cancelled
	return o.waitErr
}

func (o *remoteOperationStub) CancelTarget() error {
	o.cancelUsed = true
	close(o.cancelled)
	return o.cancelErr
}

func TestListInstancesIncludesContainersAndVMs(t *testing.T) {
	srv := &instanceServerStub{}
	b := &incusBackend{srv: srv}

	_, err := b.ListInstances(t.Context())

	require.NoError(t, err)
	assert.Equal(t, api.InstanceTypeAny, srv.listType)
}

func TestToImagesKeepsDistinctImageTypes(t *testing.T) {
	images := []api.Image{
		{
			Aliases:      []api.ImageAlias{{Name: "debian/12"}},
			Architecture: "x86_64",
			Fingerprint:  "container-fingerprint",
			Type:         "container",
		},
		{
			Aliases:      []api.ImageAlias{{Name: "debian/12"}},
			Architecture: "x86_64",
			Fingerprint:  "vm-fingerprint",
			Type:         "virtual-machine",
		},
	}

	got := toImages(images)

	require.Len(t, got, 2)
	assert.Equal(t, "container-fingerprint", got[0].Fingerprint)
	assert.Equal(t, "container", got[0].Type)
	assert.Equal(t, "vm-fingerprint", got[1].Fingerprint)
	assert.Equal(t, "virtual-machine", got[1].Type)
}

func TestCreateRequestUsesExactImageFingerprintAndType(t *testing.T) {
	req, err := createRequest(backend.CreateOptions{
		Name:        "demo",
		Image:       "debian/12",
		Fingerprint: "vm-fingerprint",
		Type:        "virtual-machine",
		Start:       true,
	})

	require.NoError(t, err)
	assert.Equal(t, api.InstanceTypeVM, req.Type)
	assert.Equal(t, "vm-fingerprint", req.Source.Fingerprint)
	assert.Empty(t, req.Source.Alias)
	assert.True(t, req.Start)
}

func TestMapErrUsesStructuredStatus(t *testing.T) {
	notFound := api.StatusErrorf(http.StatusNotFound, "missing")
	conflict := api.StatusErrorf(http.StatusConflict, "duplicate")
	invalid := api.StatusErrorf(http.StatusBadRequest, "invalid limit")

	require.ErrorIs(t, mapErr(notFound), backend.ErrNotFound)
	require.ErrorIs(t, mapErr(conflict), backend.ErrConflict)
	require.ErrorIs(t, mapErr(invalid), backend.ErrInvalid)
	assert.True(t, api.StatusErrorCheck(mapErr(notFound), http.StatusNotFound))
}

func TestMapErrMapsInvalidConfigOperationError(t *testing.T) {
	err := errors.New("Invalid config: Invalid CPU limit syntax")

	require.ErrorIs(t, mapErr(err), backend.ErrInvalid)
}

func TestCPUPercentZeroOnFirstSampleThenDeltaBased(t *testing.T) {
	b := &incusBackend{cpuSamples: make(map[string]cpuSample)}

	// First reading has no prior sample, so it reads 0.
	assert.Zero(t, b.cpuPercent("demo", 1_000_000_000, b.cpuEpochSnapshot()))

	// Pre-seed a sample one second in the past with 1e9 fewer nanos so the next
	// reading reflects ~one core fully busy over the elapsed second (≈100%).
	b.cpuSamples["demo"] = cpuSample{nanos: 1_000_000_000, at: time.Now().Add(-time.Second)}
	assert.Greater(t, b.cpuPercent("demo", 2_000_000_000, b.cpuEpochSnapshot()), 0.0)
}

func TestCPUPercentPrunesStaleSamples(t *testing.T) {
	b := &incusBackend{
		cpuSamples: map[string]cpuSample{
			"deleted": {at: time.Now().Add(-cpuSampleTTL - time.Second)},
		},
	}

	b.cpuPercent("active", 1, b.cpuEpochSnapshot())

	assert.NotContains(t, b.cpuSamples, "deleted")
	assert.Contains(t, b.cpuSamples, "active")
}

func TestCPUPercentDoesNotRecreateSampleAfterDeletion(t *testing.T) {
	b := &incusBackend{
		cpuSamples: map[string]cpuSample{"demo": {}},
	}
	epoch := b.cpuEpochSnapshot()

	b.clearCPUSample("demo")
	b.cpuPercent("demo", 1, epoch)

	assert.NotContains(t, b.cpuSamples, "demo")
}

func TestLifecycleActionsSendCorrectIncusAction(t *testing.T) {
	cases := []struct {
		name   string
		call   func(b *incusBackend) error
		action string
	}{
		{"restart", func(b *incusBackend) error { return b.RestartInstance(context.Background(), "demo") }, "restart"},
		{"pause", func(b *incusBackend) error { return b.PauseInstance(context.Background(), "demo") }, "freeze"},
		{"resume", func(b *incusBackend) error { return b.ResumeInstance(context.Background(), "demo") }, "unfreeze"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &instanceServerStub{}
			b := &incusBackend{srv: srv}
			require.NoError(t, tc.call(b))
			assert.Equal(t, tc.action, srv.stateAction)
		})
	}
}

func TestCloneInstanceWaitsWithContext(t *testing.T) {
	op := &remoteOperationStub{
		started:   make(chan struct{}),
		cancelled: make(chan struct{}),
	}
	b := &incusBackend{
		srv: &instanceServerStub{
			instance: &api.Instance{Name: "source"},
			copyOp:   op,
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		errCh <- b.CloneInstance(ctx, "source", "copy")
	}()
	<-op.started
	cancel()

	err := <-errCh

	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, op.cancelUsed)
}

func TestCloneInstancePreservesCancellationFailure(t *testing.T) {
	cancelErr := errors.New("cancel target")
	op := &remoteOperationStub{
		cancelErr: cancelErr,
		started:   make(chan struct{}),
		cancelled: make(chan struct{}),
	}
	b := &incusBackend{
		srv: &instanceServerStub{
			instance: &api.Instance{Name: "source"},
			copyOp:   op,
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		errCh <- b.CloneInstance(ctx, "source", "copy")
	}()
	<-op.started
	cancel()

	err := <-errCh

	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, cancelErr)
}

func TestDeleteInstanceRemovesCPUSampleAfterSuccessfulDelete(t *testing.T) {
	b := &incusBackend{
		srv: &instanceServerStub{
			state:    &api.InstanceState{Status: "Stopped"},
			deleteOp: &operationStub{},
		},
		cpuSamples: map[string]cpuSample{"demo": {}},
	}

	require.NoError(t, b.DeleteInstance(t.Context(), "demo"))
	assert.NotContains(t, b.cpuSamples, "demo")
}

func TestDeleteInstanceRetainsCPUSampleWhenDeleteFails(t *testing.T) {
	b := &incusBackend{
		srv: &instanceServerStub{
			state:    &api.InstanceState{Status: "Stopped"},
			deleteOp: &operationStub{waitErr: errors.New("delete failed")},
		},
		cpuSamples: map[string]cpuSample{"demo": {}},
	}

	require.Error(t, b.DeleteInstance(t.Context(), "demo"))
	assert.Contains(t, b.cpuSamples, "demo")
}

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
	assert.ErrorIs(t, srv.importReadErr, context.Canceled)
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

func TestConsoleLogReadsContent(t *testing.T) {
	srv := &instanceServerStub{consoleLog: "boot line 1\nboot line 2\n"}
	b := &incusBackend{srv: srv}

	log, err := b.ConsoleLog(t.Context(), "demo")

	require.NoError(t, err)
	assert.Equal(t, "boot line 1\nboot line 2\n", log)
}

func TestConsoleLogMapsStructuredStatus(t *testing.T) {
	b := &incusBackend{srv: &instanceServerStub{consoleErr: api.StatusErrorf(http.StatusNotFound, "missing")}}

	_, err := b.ConsoleLog(t.Context(), "ghost")

	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestConsoleLogReportsCloseFailure(t *testing.T) {
	closeErr := errors.New("close console log")
	b := &incusBackend{srv: &instanceServerStub{
		consoleLog:      "boot line\n",
		consoleCloseErr: closeErr,
	}}

	_, err := b.ConsoleLog(t.Context(), "demo")

	require.ErrorIs(t, err, closeErr)
}

func TestResizeControlMessage(t *testing.T) {
	msg := resizeControl(backend.WinSize{Cols: 120, Rows: 40})

	assert.Equal(t, "window-resize", msg.Command)
	assert.Equal(t, "120", msg.Args["width"])
	assert.Equal(t, "40", msg.Args["height"])
}

func TestListSnapshotsMapsStructuredStatus(t *testing.T) {
	b := &incusBackend{
		srv: &instanceServerStub{
			snapshotErr: api.StatusErrorf(http.StatusNotFound, "missing"),
		},
	}

	_, err := b.ListSnapshots(t.Context(), "ghost")

	require.ErrorIs(t, err, backend.ErrNotFound)
}
