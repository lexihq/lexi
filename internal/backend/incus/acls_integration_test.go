//go:build integration

package incus

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

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

// TestNetworkACLNICAttachmentGuardsDelete proves the daemon counts a NIC-device
// security.acls reference as usage (here via a profile nic — no instance
// needed) and that its refusal maps to ErrConflict, matching the fake.
func TestNetworkACLNICAttachmentGuardsDelete(t *testing.T) {
	b := newBackend(t)
	if !b.Capabilities().NetworkACLs {
		t.Skip("daemon lacks the network_acl extension")
	}
	ctx := context.Background()
	aclName := uniqueName("lxacl")
	// Bridge names map to a Linux interface (≤15 chars), so keep it short.
	netName := fmt.Sprintf("lxbr%d", time.Now().UnixNano()%100000)
	profName := uniqueName("lxprof")
	t.Cleanup(func() {
		_ = b.DeleteProfile(ctx, profName)
		_ = b.DeleteNetwork(ctx, netName)
		_ = b.DeleteNetworkACL(ctx, aclName)
	})

	require.NoError(t, b.CreateNetworkACL(ctx, aclName, "made by test"))
	// ACLs attach only to managed bridges; create a throwaway one. Explicit
	// subnet so the test doesn't depend on Incus auto-allocating one.
	require.NoError(t, b.CreateNetwork(ctx, backend.Network{
		Name: netName, Type: "bridge",
		Config: map[string]string{"ipv4.address": "10.97.0.1/24", "ipv4.nat": "true", "ipv6.address": "none"},
	}))
	require.NoError(t, b.CreateProfile(ctx, profName, "made by test"))
	// NIC-level ACLs need the nftables firewall; on xtables hosts the daemon
	// refuses the attachment itself (surfaced to users as the 400 toast), so
	// there is no usage guard to exercise — skip, don't fail.
	if err := b.AddProfileDevice(ctx, profName, "eth0", map[string]string{
		"type": "nic", "network": netName, "security.acls": aclName,
	}); err != nil {
		if strings.Contains(err.Error(), "nftables") {
			t.Skipf("NIC ACLs unsupported on this host (ok): %v", err)
		}
		t.Fatalf("add profile nic with security.acls: %v", err)
	}

	require.ErrorIs(t, b.DeleteNetworkACL(ctx, aclName), backend.ErrConflict)
	require.ErrorIs(t, b.RenameNetworkACL(ctx, aclName, uniqueName("lxacl")), backend.ErrConflict)

	require.NoError(t, b.RemoveProfileDevice(ctx, profName, "eth0"))
	require.NoError(t, b.DeleteNetworkACL(ctx, aclName))
}
