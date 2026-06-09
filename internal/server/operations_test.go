package server

import (
	"net/http"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationsPartialListsRecordedTasks(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), "GET", "/partials/operations", "", true)

	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "Creating instance")
	assert.Contains(t, res.Body.String(), "Success")
}

func TestOperationsPartialEmptyState(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/partials/operations", "", true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "No recent tasks")
}
