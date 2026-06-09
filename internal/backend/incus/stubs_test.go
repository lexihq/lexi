package incus

import (
	"context"
	"errors"
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
	networks          []api.Network         // returned by GetNetworks
	network           *api.Network          // returned by GetNetwork
	networkErr        error                 // error for network calls
	createdNet        *api.NetworksPost     // captured by CreateNetwork
	deletedNet        string                // captured by DeleteNetwork

	pools      []api.StoragePool       // returned by GetStoragePools
	pool       *api.StoragePool        // returned by GetStoragePool
	volumes    []api.StorageVolume     // returned by GetStoragePoolVolumes
	volume     *api.StorageVolume      // returned by GetStoragePoolVolume
	storageErr error                   // error for storage calls
	createdVol *api.StorageVolumesPost // captured by CreateStoragePoolVolume
	deletedVol [3]string               // pool/volType/name captured by DeleteStoragePoolVolume

	volumeSnapshots []api.StorageVolumeSnapshot // returned by GetStoragePoolVolumeSnapshots
	createdSnap     string                      // name captured by CreateStoragePoolVolumeSnapshot
	deletedSnap     string                      // name captured by DeleteStoragePoolVolumeSnapshot
	restoredVol     *api.StorageVolumePut       // captured by UpdateStoragePoolVolume

	renamedInstance  [2]string         // name/newName captured by RenameInstance
	migratedInstance *api.InstancePost // captured by MigrateInstance

	snapshotSnaps []api.InstanceSnapshot     // returned by GetInstanceSnapshots
	snapshotPost  *api.InstanceSnapshotsPost // captured by CreateInstanceSnapshot
	renamedSnap   [3]string                  // name/snap/newName captured by RenameInstanceSnapshot
	snap          *api.InstanceSnapshot      // returned by GetInstanceSnapshot
	snapExpiry    *api.InstanceSnapshotPut   // captured by UpdateInstanceSnapshot

	localImages   []api.Image                 // returned by GetImages
	imagesErr     error                       // error for image calls
	createdImage  *api.ImagesPost             // captured by CreateImage
	createImageOp incusclient.Operation       // operation returned by CreateImage
	imageCopySrc  incusclient.ImageServer     // captured by CopyImage
	imageCopied   *api.Image                  // captured by CopyImage
	imageCopyArgs *incusclient.ImageCopyArgs  // captured by CopyImage
	imageCopyOp   incusclient.RemoteOperation // operation returned by CopyImage
	deletedImage  string                      // captured by DeleteImage
	createdAlias  *api.ImageAliasesPost       // captured by CreateImageAlias
	deletedAlias  string                      // captured by DeleteImageAlias
	aliasErr      error                       // error for image alias calls

	operations    []api.Operation // returned by GetOperations
	operationsErr error           // error for GetOperations
}

func (s *instanceServerStub) GetOperations() ([]api.Operation, error) {
	return s.operations, s.operationsErr
}

func (s *instanceServerStub) CreateInstanceSnapshot(_ string, p api.InstanceSnapshotsPost) (incusclient.Operation, error) {
	s.snapshotPost = &p
	if s.snapshotErr != nil {
		return nil, s.snapshotErr
	}
	return &operationStub{}, nil
}

func (s *instanceServerStub) RenameInstanceSnapshot(name, snap string, p api.InstanceSnapshotPost) (incusclient.Operation, error) {
	s.renamedSnap = [3]string{name, snap, p.Name}
	if s.snapshotErr != nil {
		return nil, s.snapshotErr
	}
	return &operationStub{}, nil
}

func (s *instanceServerStub) GetInstanceSnapshot(string, string) (*api.InstanceSnapshot, string, error) {
	return s.snap, "etag", s.snapshotErr
}

func (s *instanceServerStub) UpdateInstanceSnapshot(_, _ string, p api.InstanceSnapshotPut, _ string) (incusclient.Operation, error) {
	s.snapExpiry = &p
	if s.snapshotErr != nil {
		return nil, s.snapshotErr
	}
	return &operationStub{}, nil
}

func (s *instanceServerStub) GetStoragePoolVolumeSnapshots(string, string, string) ([]api.StorageVolumeSnapshot, error) {
	return s.volumeSnapshots, s.storageErr
}

func (s *instanceServerStub) CreateStoragePoolVolumeSnapshot(_, _, _ string, snapshot api.StorageVolumeSnapshotsPost) (incusclient.Operation, error) {
	s.createdSnap = snapshot.Name
	if s.storageErr != nil {
		return nil, s.storageErr
	}
	return &operationStub{}, nil
}

func (s *instanceServerStub) DeleteStoragePoolVolumeSnapshot(_, _, _, name string) (incusclient.Operation, error) {
	s.deletedSnap = name
	if s.storageErr != nil {
		return nil, s.storageErr
	}
	return &operationStub{}, nil
}

func (s *instanceServerStub) UpdateStoragePoolVolume(_, _, _ string, volume api.StorageVolumePut, _ string) error {
	s.restoredVol = &volume
	return s.storageErr
}

func (s *instanceServerStub) GetStoragePools() ([]api.StoragePool, error) {
	return s.pools, s.storageErr
}

func (s *instanceServerStub) GetStoragePool(string) (*api.StoragePool, string, error) {
	return s.pool, "etag", s.storageErr
}

func (s *instanceServerStub) GetStoragePoolVolumes(string) ([]api.StorageVolume, error) {
	return s.volumes, s.storageErr
}

func (s *instanceServerStub) GetStoragePoolVolume(string, string, string) (*api.StorageVolume, string, error) {
	return s.volume, "etag", s.storageErr
}

func (s *instanceServerStub) CreateStoragePoolVolume(_ string, vol api.StorageVolumesPost) error {
	s.createdVol = &vol
	return s.storageErr
}

func (s *instanceServerStub) DeleteStoragePoolVolume(pool, volType, name string) error {
	s.deletedVol = [3]string{pool, volType, name}
	return s.storageErr
}

func (s *instanceServerStub) GetNetworks() ([]api.Network, error) {
	return s.networks, s.networkErr
}

func (s *instanceServerStub) GetNetwork(string) (*api.Network, string, error) {
	return s.network, "etag", s.networkErr
}

func (s *instanceServerStub) CreateNetwork(n api.NetworksPost) error {
	s.createdNet = &n
	return s.networkErr
}

func (s *instanceServerStub) DeleteNetwork(name string) error {
	s.deletedNet = name
	return s.networkErr
}

func (s *instanceServerStub) GetProfiles() ([]api.Profile, error) {
	return s.profiles, s.profileErr
}

func (s *instanceServerStub) GetProfile(string) (*api.Profile, string, error) {
	return s.profile, "etag", s.profileErr
}

func (s *instanceServerStub) RenameInstance(name string, p api.InstancePost) (incusclient.Operation, error) {
	s.renamedInstance = [2]string{name, p.Name}
	return &operationStub{}, nil
}

func (s *instanceServerStub) MigrateInstance(_ string, p api.InstancePost) (incusclient.Operation, error) {
	s.migratedInstance = &p
	// Mirror the real client's guard so a missing Migration flag can't pass tests.
	if !p.Migration {
		return nil, errors.New("Can't ask for a rename through MigrateInstance")
	}
	return &operationStub{}, nil
}

func (s *instanceServerStub) UpdateInstance(_ string, put api.InstancePut, _ string) (incusclient.Operation, error) {
	s.updatedPut = &put
	if s.updateOp != nil {
		return s.updateOp, nil
	}
	return &operationStub{}, nil
}

func (s *instanceServerStub) GetInstanceSnapshots(string) ([]api.InstanceSnapshot, error) {
	return s.snapshotSnaps, s.snapshotErr
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

func (s *instanceServerStub) GetImages() ([]api.Image, error) {
	return s.localImages, s.imagesErr
}

func (s *instanceServerStub) CreateImage(image api.ImagesPost, _ *incusclient.ImageCreateArgs) (incusclient.Operation, error) {
	s.createdImage = &image
	if s.imagesErr != nil {
		return nil, s.imagesErr
	}
	if s.createImageOp != nil {
		return s.createImageOp, nil
	}
	return &operationStub{}, nil
}

func (s *instanceServerStub) CopyImage(source incusclient.ImageServer, image api.Image, args *incusclient.ImageCopyArgs) (incusclient.RemoteOperation, error) {
	s.imageCopySrc = source
	s.imageCopied = &image
	s.imageCopyArgs = args
	if s.imagesErr != nil {
		return nil, s.imagesErr
	}
	return s.imageCopyOp, nil
}

func (s *instanceServerStub) DeleteImage(fingerprint string) (incusclient.Operation, error) {
	s.deletedImage = fingerprint
	if s.imagesErr != nil {
		return nil, s.imagesErr
	}
	return &operationStub{}, nil
}

func (s *instanceServerStub) CreateImageAlias(alias api.ImageAliasesPost) error {
	s.createdAlias = &alias
	return s.aliasErr
}

func (s *instanceServerStub) DeleteImageAlias(name string) error {
	s.deletedAlias = name
	return s.aliasErr
}

// imageServerStub fakes the simplestreams remote for copyImageFrom tests.
type imageServerStub struct {
	incusclient.ImageServer

	alias    *api.ImageAliasesEntry
	aliasErr error
	image    *api.Image
	imageErr error
}

func (s *imageServerStub) GetImageAlias(string) (*api.ImageAliasesEntry, string, error) {
	return s.alias, "", s.aliasErr
}

func (s *imageServerStub) GetImage(string) (*api.Image, string, error) {
	return s.image, "", s.imageErr
}

type operationStub struct {
	incusclient.Operation

	waitErr         error
	waitContextUsed bool
	cancelUsed      bool
	onWait          func()
	get             api.Operation // returned by Get after waiting
}

func (o *operationStub) Get() api.Operation {
	return o.get
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
