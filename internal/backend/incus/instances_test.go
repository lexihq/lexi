package incus

import (
	"context"
	"errors"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
