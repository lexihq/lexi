package fake

import (
	"bytes"
	"strings"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecEchoesStdin(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	var out bytes.Buffer
	require.NoError(t, b.Exec(ctx(), "demo", backend.ExecRequest{
		Stdin:  strings.NewReader("hello\n"),
		Stdout: &out,
	}))
	assert.Equal(t, "hello\n", out.String(), "fake exec should echo stdin to stdout")

	// Missing instance → ErrNotFound, before any streaming.
	err := b.Exec(ctx(), "ghost", backend.ExecRequest{Stdin: strings.NewReader(""), Stdout: &out})
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestConsoleLog(t *testing.T) {
	b := New()
	assert.True(t, b.Capabilities(ctx()).Console, "fake should advertise console")
	mustCreate(t, b, "demo")

	log, err := b.ConsoleLog(ctx(), "demo")
	require.NoError(t, err)
	assert.NotEmpty(t, log, "console log should return canned text")

	_, err = b.ConsoleLog(ctx(), "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)
}
