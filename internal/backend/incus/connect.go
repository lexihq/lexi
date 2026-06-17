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
// default remote (overridable with LEXI_INCUS_REMOTE). This works against an
// HTTPS+TLS remote in dev (e.g. a Lima VM) and a unix socket on a Linux deploy
// (where LoadConfig returns a default config whose "local" remote is the system
// socket). A genuinely missing config falls back to the socket directly; a
// present-but-invalid config is surfaced rather than silently masked.
// Alongside the connection it returns the resolved remote's name and address
// and the loaded config (nil on the no-config socket fallback), which the
// multi-remote layer builds on.
func Connect() (incusclient.InstanceServer, string, string, *cliconfig.Config, error) {
	conf, err := cliconfig.LoadConfig("")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			srv, err := incusclient.ConnectIncusUnix("", nil)
			return srv, "local", "unix://", nil, err
		}
		return nil, "", "", nil, fmt.Errorf("load incus cli config: %w", err)
	}
	remote := conf.DefaultRemote
	if r := os.Getenv("LEXI_INCUS_REMOTE"); r != "" {
		remote = r
	}
	srv, err := conf.GetInstanceServer(remote)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("connect to incus remote %q: %w", remote, err)
	}
	return srv, remote, conf.Remotes[remote].Addr, conf, nil
}
