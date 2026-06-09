package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/require"
)

func TestRenameInstanceRedirects(t *testing.T) {
	b := newInstance(t)
	res := formRequest(t, New(b), "/instances/demo/rename", url.Values{"new_name": {"renamed"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	_, err := b.GetInstance(t.Context(), "renamed")
	require.NoError(t, err)
	_, err = b.GetInstance(t.Context(), "demo")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestRenameInstanceBlankNameIs400(t *testing.T) {
	res := formRequest(t, New(newInstance(t)), "/instances/demo/rename", url.Values{"new_name": {"  "}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestRenameInstanceConflictIs409(t *testing.T) {
	b := newInstance(t)
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "other"}))
	res := formRequest(t, New(b), "/instances/other/rename", url.Values{"new_name": {"demo"}}, false)
	assertStatus(t, res, http.StatusConflict)
}

func TestMoveInstanceRedirects(t *testing.T) {
	res := formRequest(t, New(newInstance(t)), "/instances/demo/move", url.Values{"pool": {"zfs0"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
}

func TestMoveInstanceBlankPoolIs400(t *testing.T) {
	res := formRequest(t, New(newInstance(t)), "/instances/demo/move", url.Values{"pool": {"  "}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestMoveInstanceUnknownPoolIs404(t *testing.T) {
	res := formRequest(t, New(newInstance(t)), "/instances/demo/move", url.Values{"pool": {"ghostpool"}}, false)
	assertStatus(t, res, http.StatusNotFound)
}
