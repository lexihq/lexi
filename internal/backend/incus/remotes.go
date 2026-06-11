package incus

import (
	"context"

	"github.com/adam/lxcon/internal/backend"
)

// ListRemotes reports the connected remote. Single-remote for now: the
// multi-remote dial (Batch C of the remotes plan) replaces this with the full
// CLI-config set, and Capabilities.Remotes stays false until then so the UI
// never offers a switch the driver can't honor.
func (b *incusBackend) ListRemotes(_ context.Context) ([]backend.Remote, error) {
	return []backend.Remote{{Name: b.remoteName, Addr: b.remoteAddr, Current: true}}, nil
}
