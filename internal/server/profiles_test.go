package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeProfileOrderKeepsExistingThenAppends(t *testing.T) {
	assert.Equal(t, []string{"default", "gpu"},
		mergeProfileOrder([]string{"default", "gpu"}, []string{"gpu", "default"}))
	assert.Equal(t, []string{"default", "gpu", "web"},
		mergeProfileOrder([]string{"default", "gpu"}, []string{"gpu", "web", "default"}))
	assert.Equal(t, []string{"default"},
		mergeProfileOrder([]string{"default", "gpu"}, []string{"default"}))
}

func TestProfilesPageRenders(t *testing.T) {
	b := fake.New()
	res := request(t, New(b), "GET", "/profiles", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, "default")
	assert.Contains(t, body, "gpu")
}

func TestProfileDetailRenders(t *testing.T) {
	b := fake.New()
	res := request(t, New(b), "GET", "/profiles/gpu", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "gpu")
}

func TestSetInstanceProfilesReturnsUpdatedControlOnHTMX(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	res := formRequest(t, New(b), "/instances/demo/profiles", url.Values{"profile": {"default", "gpu"}}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "gpu")
	inst, err := b.GetInstance(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"default", "gpu"}, inst.Profiles)
}

func TestSetInstanceProfilesUnknownInstanceIs404(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/instances/ghost/profiles", url.Values{"profile": {"default"}}, true)
	assertStatus(t, res, http.StatusNotFound)
}
