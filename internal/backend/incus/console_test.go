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

func TestConsoleLogReadsContent(t *testing.T) {
	srv := &instanceServerStub{consoleLog: "boot line 1\nboot line 2\n"}
	b := &incusBackend{srv: srv}

	log, err := b.ConsoleLog(t.Context(), "demo")

	require.NoError(t, err)
	assert.Equal(t, "boot line 1\nboot line 2\n", log)
}

func TestConsoleLogMapsStructuredStatus(t *testing.T) {
	b := &incusBackend{srv: &instanceServerStub{consoleErr: api.StatusErrorf(http.StatusNotFound, "missing")}}

	_, err := b.ConsoleLog(t.Context(), "ghost")

	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestConsoleLogReportsCloseFailure(t *testing.T) {
	closeErr := errors.New("close console log")
	b := &incusBackend{srv: &instanceServerStub{
		consoleLog:      "boot line\n",
		consoleCloseErr: closeErr,
	}}

	_, err := b.ConsoleLog(t.Context(), "demo")

	require.ErrorIs(t, err, closeErr)
}

func TestResizeControlMessage(t *testing.T) {
	msg := resizeControl(backend.WinSize{Cols: 120, Rows: 40})

	assert.Equal(t, "window-resize", msg.Command)
	assert.Equal(t, "120", msg.Args["width"])
	assert.Equal(t, "40", msg.Args["height"])
}
