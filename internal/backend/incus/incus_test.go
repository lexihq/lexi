package incus

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
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
	snapshotErr    error
	listType       api.InstanceType
	state          *api.InstanceState
	instance       *api.Instance
	deleteOp       incusclient.Operation
	copyOp         incusclient.RemoteOperation
	backupOp       incusclient.Operation
	backupDeleteOp incusclient.Operation
	backupBytes    []byte
	deletedBackup  string
	importOp       incusclient.Operation
	importedName   string
	importedBytes  []byte
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
	s.importedBytes, _ = io.ReadAll(args.BackupFile)
	return s.importOp, nil
}

type operationStub struct {
	incusclient.Operation
	waitErr         error
	waitContextUsed bool
}

func (o *operationStub) WaitContext(context.Context) error {
	o.waitContextUsed = true
	return o.waitErr
}

type remoteOperationStub struct {
	incusclient.RemoteOperation
	waitErr    error
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
	return nil
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
	assert.NotEmpty(t, srv.deletedBackup, "the temporary backup should be deleted afterwards")
}

func TestImportInstanceCreatesFromBackup(t *testing.T) {
	srv := &instanceServerStub{importOp: &operationStub{}}
	b := &incusBackend{srv: srv}

	require.NoError(t, b.ImportInstance(t.Context(), "restored", strings.NewReader("tarball-bytes")))

	assert.Equal(t, "restored", srv.importedName, "destination name should be passed through")
	assert.Equal(t, "tarball-bytes", string(srv.importedBytes), "the reader should stream to the backup file")
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
