package incus

import (
	"net/http"
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
	snapshotErr error
	listType    api.InstanceType
}

func (s *instanceServerStub) GetInstanceSnapshots(string) ([]api.InstanceSnapshot, error) {
	return nil, s.snapshotErr
}

func (s *instanceServerStub) GetInstancesFull(instanceType api.InstanceType) ([]api.InstanceFull, error) {
	s.listType = instanceType
	return nil, nil
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

	require.ErrorIs(t, mapErr(notFound), backend.ErrNotFound)
	require.ErrorIs(t, mapErr(conflict), backend.ErrConflict)
	assert.True(t, api.StatusErrorCheck(mapErr(notFound), http.StatusNotFound))
}

func TestCPUPercentZeroOnFirstSampleThenDeltaBased(t *testing.T) {
	b := &incusBackend{cpuSamples: make(map[string]cpuSample)}

	// First reading has no prior sample, so it reads 0.
	assert.Zero(t, b.cpuPercent("demo", 1_000_000_000))

	// Pre-seed a sample one second in the past with 1e9 fewer nanos so the next
	// reading reflects ~one core fully busy over the elapsed second (≈100%).
	b.cpuSamples["demo"] = cpuSample{nanos: 1_000_000_000, at: time.Now().Add(-time.Second)}
	assert.Greater(t, b.cpuPercent("demo", 2_000_000_000), 0.0)
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
