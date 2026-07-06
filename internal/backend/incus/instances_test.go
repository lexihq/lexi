package incus

import (
	"context"
	"errors"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRebuildInstancePrefersFingerprint(t *testing.T) {
	srv := &instanceServerStub{rebuildOp: &operationStub{}}
	b := &incusBackend{srv: srv}

	require.NoError(t, b.RebuildInstance(t.Context(), "demo", "alpine/3.20", "fp123"))

	require.NotNil(t, srv.rebuildReq)
	assert.Equal(t, "image", srv.rebuildReq.Source.Type)
	assert.Equal(t, imagesRemote, srv.rebuildReq.Source.Server)
	assert.Equal(t, "simplestreams", srv.rebuildReq.Source.Protocol)
	assert.Equal(t, "fp123", srv.rebuildReq.Source.Fingerprint)
	assert.Empty(t, srv.rebuildReq.Source.Alias)
}

func TestRebuildInstanceFallsBackToAlias(t *testing.T) {
	srv := &instanceServerStub{rebuildOp: &operationStub{}}
	b := &incusBackend{srv: srv}

	require.NoError(t, b.RebuildInstance(t.Context(), "demo", "alpine/3.20", ""))

	require.NotNil(t, srv.rebuildReq)
	assert.Equal(t, "alpine/3.20", srv.rebuildReq.Source.Alias)
	assert.Empty(t, srv.rebuildReq.Source.Fingerprint)
}

func TestRebuildInstanceNoImageIsInvalid(t *testing.T) {
	b := &incusBackend{srv: &instanceServerStub{}}
	err := b.RebuildInstance(t.Context(), "demo", "", "")
	require.ErrorIs(t, err, backend.ErrInvalid)
}

func TestListInstancesIncludesContainersAndVMs(t *testing.T) {
	srv := &instanceServerStub{}
	b := &incusBackend{srv: srv}

	_, err := b.ListInstances(t.Context())

	require.NoError(t, err)
	assert.Equal(t, api.InstanceTypeAny, srv.listType)
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

func TestCreateRequestMapsProfilesPoolNetworkConfig(t *testing.T) {
	req, err := createRequest(backend.CreateOptions{
		Name: "demo", Image: "debian/12",
		Profiles: []string{"default", "gpu"},
		Pool:     "fast0",
		Network:  "incusbr0",
		Config:   map[string]string{"limits.cpu": "2"},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"default", "gpu"}, req.Profiles)
	assert.Equal(t, "2", req.Config["limits.cpu"])
	assert.Equal(t, map[string]string{"type": "disk", "path": "/", "pool": "fast0"}, req.Devices["root"])
	assert.Equal(t, map[string]string{"type": "nic", "name": "eth0", "network": "incusbr0"}, req.Devices["eth0"])
}

func TestCreateRequestZeroOptionsSendNoOverrides(t *testing.T) {
	req, err := createRequest(backend.CreateOptions{Name: "demo", Image: "debian/12"})

	require.NoError(t, err)
	assert.Nil(t, req.Profiles, "empty profiles keep the daemon default")
	assert.Nil(t, req.Devices, "no device overrides without pool/network")
	assert.Nil(t, req.Config)

	// A non-nil empty slice must also stay unset: the daemon applies the
	// default profile only for nil, while [] means "no profiles at all".
	req, err = createRequest(backend.CreateOptions{Name: "demo", Image: "debian/12", Profiles: []string{}})
	require.NoError(t, err)
	assert.Nil(t, req.Profiles)
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
		cpuSamples: map[string]cpuSample{"//demo": {}},
	}

	require.NoError(t, b.DeleteInstance(t.Context(), "demo"))
	assert.NotContains(t, b.cpuSamples, "//demo")
}

func TestDeleteInstanceRetainsCPUSampleWhenDeleteFails(t *testing.T) {
	b := &incusBackend{
		srv: &instanceServerStub{
			state:    &api.InstanceState{Status: "Stopped"},
			deleteOp: &operationStub{waitErr: errors.New("delete failed")},
		},
		cpuSamples: map[string]cpuSample{"/demo": {}},
	}

	require.Error(t, b.DeleteInstance(t.Context(), "demo"))
	assert.Contains(t, b.cpuSamples, "/demo")
}
