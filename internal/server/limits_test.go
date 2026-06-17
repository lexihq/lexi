package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateLimitsHXReturnsForm(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := formRequest(t, New(b), "/instances/demo/limits", url.Values{"cpu": {"2"}, "memory": {"2GiB"}}, true)

	assert.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	assert.Contains(t, body, "2GiB")
	assert.Contains(t, body, `value="2"`)

	inst, err := b.GetInstance(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "2", inst.LimitsCPU)
	assert.Equal(t, "2GiB", inst.LimitsMemory)
}
