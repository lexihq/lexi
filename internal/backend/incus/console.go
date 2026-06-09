package incus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/adam/lxcon/internal/backend"
	"github.com/gorilla/websocket"
	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

// ConsoleLog reads the instance's console log buffer into a string.
func (b *incusBackend) ConsoleLog(_ context.Context, name string) (string, error) {
	rc, err := b.srv.GetInstanceConsoleLog(name, &incusclient.InstanceConsoleLogArgs{})
	if err != nil {
		return "", fmt.Errorf("get console log of %q: %w", name, mapErr(err))
	}

	content, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return "", fmt.Errorf("read console log of %q: %w", name, err)
	}
	return string(content), nil
}

// Exec runs an interactive command (defaulting to /bin/sh, which the curated
// images all provide) bridging req.Stdin/Stdout to a single PTY. Window resizes
// from req.Resize are forwarded over the exec control socket until the session
// ends.
func (b *incusBackend) Exec(ctx context.Context, name string, req backend.ExecRequest) error {
	command := req.Command
	if len(command) == 0 {
		command = []string{"/bin/sh"}
	}

	dataDone := make(chan bool)
	op, err := b.srv.ExecInstance(name, api.InstanceExecPost{
		Command:     command,
		WaitForWS:   true,
		Interactive: true,
		Width:       req.Width,
		Height:      req.Height,
	}, &incusclient.InstanceExecArgs{
		Stdin:    req.Stdin,
		Stdout:   req.Stdout,
		Control:  execControl(ctx, req.Resize),
		DataDone: dataDone,
	})
	if err != nil {
		return fmt.Errorf("exec on %q: %w", name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("exec on %q: %w", name, mapErr(err))
	}

	// Wait for the I/O streams to flush before returning so the caller can close
	// its side cleanly; honor cancellation so a dropped client never wedges here.
	select {
	case <-dataDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// execControl returns a control-socket handler that forwards window resizes from
// resize as exec control messages. It returns nil when resize is nil so the
// client skips control handling entirely.
func execControl(ctx context.Context, resize <-chan backend.WinSize) func(*websocket.Conn) {
	if resize == nil {
		return nil
	}
	return func(conn *websocket.Conn) {
		for {
			select {
			case <-ctx.Done():
				return
			case size, ok := <-resize:
				if !ok {
					return
				}
				if err := conn.WriteJSON(resizeControl(size)); err != nil {
					return
				}
			}
		}
	}
}

// resizeControl builds the exec control message for a window resize.
func resizeControl(size backend.WinSize) api.InstanceExecControl {
	return api.InstanceExecControl{
		Command: "window-resize",
		Args: map[string]string{
			"width":  strconv.Itoa(size.Cols),
			"height": strconv.Itoa(size.Rows),
		},
	}
}
