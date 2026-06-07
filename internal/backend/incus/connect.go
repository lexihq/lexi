// Package incus implements the tier-1 backend.Backend over the Incus Go client.
package incus

import (
	"errors"
	"fmt"
	"os"

	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/cliconfig"
)

// Connect mirrors the `incus` CLI: it loads the CLI config and connects to the
// default remote (overridable with LXCON_INCUS_REMOTE). This works against an
// HTTPS+TLS remote in dev (e.g. a Lima VM) and a unix socket on a Linux deploy
// (where LoadConfig returns a default config whose "local" remote is the system
// socket). A genuinely missing config falls back to the socket directly; a
// present-but-invalid config is surfaced rather than silently masked.
func Connect() (incusclient.InstanceServer, error) {
	conf, err := cliconfig.LoadConfig("")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return incusclient.ConnectIncusUnix("", nil)
		}
		return nil, fmt.Errorf("load incus cli config: %w", err)
	}
	remote := conf.DefaultRemote
	if r := os.Getenv("LXCON_INCUS_REMOTE"); r != "" {
		remote = r
	}
	srv, err := conf.GetInstanceServer(remote)
	if err != nil {
		return nil, fmt.Errorf("connect to incus remote %q: %w", remote, err)
	}
	return srv, nil
}
