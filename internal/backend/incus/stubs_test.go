package incus

import (
	"context"
	"io"
	"strings"

	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

// Shared test stubs for the incus package: an InstanceServer fake and the
// operation fakes the driver waits on. Injected into incusBackend.srv by the
// per-feature *_test.go files in this package.

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
	profiles          []api.Profile         // returned by GetProfiles
	profile           *api.Profile          // returned by GetProfile
	profileErr        error                 // error for GetProfiles/GetProfile
	updatedPut        *api.InstancePut      // captured by UpdateInstance
	updateOp          incusclient.Operation // operation returned by UpdateInstance
}

func (s *instanceServerStub) GetProfiles() ([]api.Profile, error) {
	return s.profiles, s.profileErr
}

func (s *instanceServerStub) GetProfile(string) (*api.Profile, string, error) {
	return s.profile, "etag", s.profileErr
}

func (s *instanceServerStub) UpdateInstance(_ string, put api.InstancePut, _ string) (incusclient.Operation, error) {
	s.updatedPut = &put
	if s.updateOp != nil {
		return s.updateOp, nil
	}
	return &operationStub{}, nil
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
