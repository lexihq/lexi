//go:build integration

package incus

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConsoleLogReadsForRunningInstance starts a throwaway instance and reads
// its console log without error (content may be empty depending on the image).
func TestConsoleLogReadsForRunningInstance(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("console")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage, Start: true}))

	_, err := b.ConsoleLog(ctx, name)
	require.NoError(t, err)
}

// TestExecRunsCommandWithResize opens an interactive shell on a running
// instance, seeds a window size, runs a command, and asserts its output came
// back through the stdout bridge.
func TestExecRunsCommandWithResize(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("exec")
	t.Cleanup(func() { cleanupInstance(t, b, name) })

	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage, Start: true}))

	stdinR, stdinW := io.Pipe()
	var out bytes.Buffer
	resize := make(chan backend.WinSize, 1)
	resize <- backend.WinSize{Cols: 100, Rows: 40}

	done := make(chan error, 1)
	go func() {
		done <- b.Exec(ctx, name, backend.ExecRequest{
			Command: []string{"/bin/sh"},
			Stdin:   stdinR,
			Stdout:  &out,
			Resize:  resize,
			Width:   80,
			Height:  24,
		})
	}()

	if _, err := io.WriteString(stdinW, "echo lexi-exec-ok\n"); err != nil {
		t.Fatalf("write command: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := io.WriteString(stdinW, "exit\n"); err != nil {
		t.Fatalf("write exit: %v", err)
	}
	require.NoError(t, stdinW.Close())

	require.NoError(t, <-done)
	assert.Contains(t, out.String(), "lexi-exec-ok")
}
