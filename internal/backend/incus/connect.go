// Package incus implements the tier-1 backend.Backend over the Incus Go client.
package incus

import (
	"os"

	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/cliconfig"
)

// Connect mirrors the `incus` CLI: it loads the CLI config and connects to the
// default remote (overridable with LXCON_INCUS_REMOTE). This works against an
// HTTPS+TLS remote in dev (e.g. a Lima VM) and a unix socket on a Linux deploy.
// If no CLI config can be loaded at all, it falls back to the system socket.
func Connect() (incusclient.InstanceServer, error) {
	conf, err := cliconfig.LoadConfig("")
	if err != nil {
		return incusclient.ConnectIncusUnix("", nil)
	}
	remote := conf.DefaultRemote
	if r := os.Getenv("LXCON_INCUS_REMOTE"); r != "" {
		remote = r
	}
	return conf.GetInstanceServer(remote)
}
