//go:build integration

package incus

import (
	"context"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNetworkACLCRUDRoundTrip creates a throwaway ACL, edits its description
// and rules (versioned), renames it, and deletes it.
func TestNetworkACLCRUDRoundTrip(t *testing.T) {
	b := newBackend(t)
	if !b.Capabilities().NetworkACLs {
		t.Skip("daemon lacks the network_acl extension")
	}
	ctx := context.Background()
	name := uniqueName("lxacl")
	renamed := uniqueName("lxacl")
	t.Cleanup(func() { _ = b.DeleteNetworkACL(ctx, name); _ = b.DeleteNetworkACL(ctx, renamed) })

	require.NoError(t, b.CreateNetworkACL(ctx, name, "made by test"))
	require.ErrorIs(t, b.CreateNetworkACL(ctx, name, ""), backend.ErrConflict)

	acl, err := b.GetNetworkACL(ctx, name)
	require.NoError(t, err)
	require.NotEmpty(t, acl.Version)

	rule := backend.NetworkACLRule{Action: "allow", Protocol: "tcp", DestinationPort: "443", State: "enabled"}
	require.NoError(t, b.UpdateNetworkACL(ctx, name, "edited", []backend.NetworkACLRule{rule}, nil, acl.Version))
	got, err := b.GetNetworkACL(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	require.Len(t, got.Ingress, 1)
	assert.Equal(t, "443", got.Ingress[0].DestinationPort)

	// Stale etag conflicts.
	require.ErrorIs(t, b.UpdateNetworkACL(ctx, name, "stale", nil, nil, acl.Version), backend.ErrConflict)

	require.NoError(t, b.RenameNetworkACL(ctx, name, renamed))
	_, err = b.GetNetworkACL(ctx, name)
	require.ErrorIs(t, err, backend.ErrNotFound)

	require.NoError(t, b.DeleteNetworkACL(ctx, renamed))
	require.ErrorIs(t, b.DeleteNetworkACL(ctx, renamed), backend.ErrNotFound)
}
