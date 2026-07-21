package incus

import (
	"errors"
	"net/http"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapErrUsesStructuredStatus(t *testing.T) {
	notFound := api.StatusErrorf(http.StatusNotFound, "missing")
	conflict := api.StatusErrorf(http.StatusConflict, "duplicate")
	invalid := api.StatusErrorf(http.StatusBadRequest, "invalid limit")

	require.ErrorIs(t, mapErr(notFound), backend.ErrNotFound)
	require.ErrorIs(t, mapErr(conflict), backend.ErrConflict)
	require.ErrorIs(t, mapErr(invalid), backend.ErrInvalid)
	assert.True(t, api.StatusErrorCheck(mapErr(notFound), http.StatusNotFound))
}

func TestMapErrMapsEtagRaceToConflict(t *testing.T) {
	// The real optimistic-concurrency failure: a conditional PUT against a
	// stale etag comes back as 412, which must surface as ErrConflict (409)
	// like the fake's fabricated conflicts do.
	err := api.StatusErrorf(http.StatusPreconditionFailed, "ETag doesn't match")

	require.ErrorIs(t, mapErr(err), backend.ErrConflict)
}

func TestMapErrMapsUniqueConstraintTextToConflict(t *testing.T) {
	// Some create paths (projects, raced volume imports) surface the database
	// constraint as flattened operation text with no HTTP status.
	err := errors.New(`Failed creating project: UNIQUE constraint failed: projects.name`)

	require.ErrorIs(t, mapErr(err), backend.ErrConflict)
}

func TestMapErrMapsInvalidConfigOperationError(t *testing.T) {
	err := errors.New("Invalid config: Invalid CPU limit syntax")

	require.ErrorIs(t, mapErr(err), backend.ErrInvalid)
}

func TestMapErrMapsMissingExtensionToUnsupported(t *testing.T) {
	err := errors.New(`The server is missing the required "custom_volume_backup" API extension`)

	require.ErrorIs(t, mapErr(err), backend.ErrUnsupported)
}
