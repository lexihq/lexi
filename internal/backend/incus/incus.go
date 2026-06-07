package incus

import (
	"fmt"

	"github.com/adam/lxcon/internal/backend"

	incusclient "github.com/lxc/incus/v6/client"
)

// incusBackend implements backend.Backend over the Incus Go client.
type incusBackend struct {
	srv  incusclient.InstanceServer
	caps backend.Capabilities
}

// New connects to Incus (default remote) and probes the server to populate
// capabilities. It returns a clear error if the daemon is unreachable.
func New() (*incusBackend, error) {
	srv, err := Connect()
	if err != nil {
		return nil, fmt.Errorf("connect to incus: %w", err)
	}
	info, _, err := srv.GetServer()
	if err != nil {
		return nil, fmt.Errorf("probe incus server: %w", err)
	}
	return &incusBackend{
		srv: srv,
		caps: backend.Capabilities{
			Tier:       backend.TierIncus,
			ServerInfo: fmt.Sprintf("Incus %s", info.Environment.ServerVersion),
			Snapshots:  true,
			Clone:      true,
		},
	}, nil
}

func (b *incusBackend) Capabilities() backend.Capabilities { return b.caps }
