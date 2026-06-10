package fake

import (
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkACLCRUDRoundTrip(t *testing.T) {
	f := New()

	acls, err := f.ListNetworkACLs(ctx())
	require.NoError(t, err)
	assert.Empty(t, acls)

	require.NoError(t, f.CreateNetworkACL(ctx(), "web", "web traffic"))
	require.ErrorIs(t, f.CreateNetworkACL(ctx(), "web", ""), backend.ErrConflict)
	require.ErrorIs(t, f.CreateNetworkACL(ctx(), "bad name", ""), backend.ErrInvalid)
	require.ErrorIs(t, f.CreateNetworkACL(ctx(), "@reserved", ""), backend.ErrInvalid)
	require.ErrorIs(t, f.CreateNetworkACL(ctx(), "under_score", ""), backend.ErrInvalid)

	acl, err := f.GetNetworkACL(ctx(), "web")
	require.NoError(t, err)
	require.NotEmpty(t, acl.Version)

	// Update replaces description + rules, conditionally on the version.
	rule := backend.NetworkACLRule{Action: "allow", Protocol: "tcp", DestinationPort: "443", State: "enabled"}
	require.NoError(t, f.UpdateNetworkACL(ctx(), "web", "edited", []backend.NetworkACLRule{rule}, nil, acl.Version))
	require.ErrorIs(t, f.UpdateNetworkACL(ctx(), "web", "stale", nil, nil, acl.Version), backend.ErrConflict)

	got, err := f.GetNetworkACL(ctx(), "web")
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	require.Len(t, got.Ingress, 1)
	assert.Equal(t, "443", got.Ingress[0].DestinationPort)
	assert.Empty(t, got.Egress)

	// Rename moves the ACL; collisions and bad names are rejected.
	require.NoError(t, f.CreateNetworkACL(ctx(), "other", ""))
	require.ErrorIs(t, f.RenameNetworkACL(ctx(), "web", "other"), backend.ErrConflict)
	require.ErrorIs(t, f.RenameNetworkACL(ctx(), "web", "bad name"), backend.ErrInvalid)
	require.NoError(t, f.RenameNetworkACL(ctx(), "web", "frontend"))
	_, err = f.GetNetworkACL(ctx(), "web")
	require.ErrorIs(t, err, backend.ErrNotFound)
	got, err = f.GetNetworkACL(ctx(), "frontend")
	require.NoError(t, err)
	assert.Len(t, got.Ingress, 1, "rules carry across rename")

	require.NoError(t, f.DeleteNetworkACL(ctx(), "frontend"))
	_, err = f.GetNetworkACL(ctx(), "frontend")
	require.ErrorIs(t, err, backend.ErrNotFound)
	require.ErrorIs(t, f.DeleteNetworkACL(ctx(), "ghost"), backend.ErrNotFound)
}

func TestNetworkACLDeleteRefusedWhenReferenced(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateNetworkACL(ctx(), "web", ""))

	// Reference the ACL from the seeded managed network's security.acls.
	n, err := f.GetNetwork(ctx(), "incusbr0")
	require.NoError(t, err)
	cfg := n.Config
	cfg["security.acls"] = "web"
	require.NoError(t, f.UpdateNetwork(ctx(), "incusbr0", n.Description, cfg, ""))

	acl, err := f.GetNetworkACL(ctx(), "web")
	require.NoError(t, err)
	assert.Contains(t, acl.UsedBy, "/1.0/networks/incusbr0")
	require.ErrorIs(t, f.DeleteNetworkACL(ctx(), "web"), backend.ErrConflict)

	// An attached ACL also cannot be renamed, like the daemon.
	require.ErrorIs(t, f.RenameNetworkACL(ctx(), "web", "web2"), backend.ErrConflict)

	// Detach and delete cleanly.
	delete(cfg, "security.acls")
	require.NoError(t, f.UpdateNetwork(ctx(), "incusbr0", n.Description, cfg, ""))
	require.NoError(t, f.DeleteNetworkACL(ctx(), "web"))
}
