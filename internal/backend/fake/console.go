package fake

import (
	"context"
	"fmt"
	"io"

	"github.com/adam/lxcon/internal/backend"
)

// Exec echoes stdin back to stdout for an existing instance, which is enough to
// assert the WebSocket bridge wiring without a live daemon. It ignores resize
// events. The instance check happens before any streaming.
func (f *Fake) Exec(_ context.Context, name string, req backend.ExecRequest) error {
	f.mu.Lock()
	_, ok := f.instances[name]
	f.mu.Unlock()
	if !ok {
		return notFound(name)
	}
	_, err := io.Copy(req.Stdout, req.Stdin)
	return err
}

// ConsoleLog returns canned console output for an existing instance so handler
// and UI tests can assert the logs panel without a live daemon.
func (f *Fake) ConsoleLog(_ context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[name]; !ok {
		return "", notFound(name)
	}
	return fmt.Sprintf("[fake console] %s booted\nlogin: ", name), nil
}
