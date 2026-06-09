package server

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstanceURLPathEscapesName(t *testing.T) {
	assert.Equal(t, "/instances/demo", instanceURL("demo"))
	assert.Equal(t, "/instances/%2F%2Fevil.example", instanceURL("//evil.example"))
}

func TestStatusForSentinels(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	// Missing instance → 404.
	missing := request(t, New(b), "GET", "/instances/ghost", "", true)
	assert.Equal(t, http.StatusNotFound, missing.Code)

	// Duplicate create → 409.
	dup := formRequest(t, New(b), "/instances", url.Values{"name": {"demo"}, "image": {"fake-debian-12-aarch64"}}, true)
	assert.Equal(t, http.StatusConflict, dup.Code)

	assert.Equal(t, http.StatusBadRequest, statusFor(fmt.Errorf("invalid limits: %w", backend.ErrInvalid)))
}
