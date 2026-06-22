package incus

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

func (b *incusBackend) ListNetworkACLs(ctx context.Context) ([]backend.NetworkACL, error) {
	acls, err := b.project(ctx).GetNetworkACLs()
	if err != nil {
		return nil, fmt.Errorf("list network ACLs: %w", mapErr(err))
	}
	out := make([]backend.NetworkACL, 0, len(acls))
	for i := range acls {
		out = append(out, toNetworkACL(&acls[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (b *incusBackend) GetNetworkACL(ctx context.Context, name string) (backend.NetworkACL, error) {
	acl, etag, err := b.project(ctx).GetNetworkACL(name)
	if err != nil {
		return backend.NetworkACL{}, fmt.Errorf("get network ACL %q: %w", name, mapErr(err))
	}
	out := toNetworkACL(acl)
	out.Version = etag
	return out, nil
}

func (b *incusBackend) CreateNetworkACL(ctx context.Context, acl backend.NetworkACL) error {
	post := api.NetworkACLsPost{}
	post.Name = acl.Name
	post.Description = acl.Description
	if err := b.project(ctx).CreateNetworkACL(post); err != nil {
		// The daemon reports a duplicate as a plain 400 ("The network ACL
		// already exists"), which mapErr's typed BadRequest branch would turn
		// into ErrInvalid before the string fallback can see it.
		if strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("network ACL %q already exists: %w", acl.Name, backend.ErrConflict)
		}
		return fmt.Errorf("create network ACL %q: %w", acl.Name, mapErr(err))
	}
	return nil
}

// UpdateNetworkACL replaces the ACL's description and rule lists via
// GET-preserve-PUT, so the ACL's Config map is never dropped. The version is
// the GetNetworkACL etag (412 → ErrConflict); an empty version updates
// unconditionally. UpdateNetworkACL is synchronous.
func (b *incusBackend) UpdateNetworkACL(ctx context.Context, name, description string, ingress, egress []backend.NetworkACLRule, version string) error {
	acl, _, err := b.project(ctx).GetNetworkACL(name)
	if err != nil {
		return fmt.Errorf("get network ACL %q: %w", name, mapErr(err))
	}
	put := acl.Writable()
	put.Description = description
	put.Ingress = toAPIRules(ingress)
	put.Egress = toAPIRules(egress)
	if err := b.project(ctx).UpdateNetworkACL(name, put, version); err != nil {
		return fmt.Errorf("update network ACL %q: %w", name, mapErr(err))
	}
	return nil
}

// RenameNetworkACL renames an ACL. The source is pre-checked for use (the
// daemon refuses renaming an attached ACL with an untyped error) and the
// target name for collisions, so both surface as clean sentinels.
func (b *incusBackend) RenameNetworkACL(ctx context.Context, name, newName string) error {
	acl, _, err := b.project(ctx).GetNetworkACL(name)
	if err != nil {
		return fmt.Errorf("get network ACL %q: %w", name, mapErr(err))
	}
	if n := len(acl.UsedBy); n > 0 {
		return fmt.Errorf("network ACL %q is in use by %d object(s): %w", name, n, backend.ErrConflict)
	}
	acls, err := b.ListNetworkACLs(ctx)
	if err != nil {
		return err
	}
	for _, a := range acls {
		if a.Name == newName {
			return fmt.Errorf("network ACL %q already exists: %w", newName, backend.ErrConflict)
		}
	}
	if err := b.project(ctx).RenameNetworkACL(name, api.NetworkACLPost{Name: newName}); err != nil {
		return fmt.Errorf("rename network ACL %q: %w", name, mapErr(err))
	}
	return nil
}

// DeleteNetworkACL pre-checks UsedBy so a referenced ACL conflicts cleanly
// (the daemon's in-use error is an untyped 400).
func (b *incusBackend) DeleteNetworkACL(ctx context.Context, name string) error {
	acl, _, err := b.project(ctx).GetNetworkACL(name)
	if err != nil {
		return fmt.Errorf("get network ACL %q: %w", name, mapErr(err))
	}
	if n := len(acl.UsedBy); n > 0 {
		return fmt.Errorf("network ACL %q is in use by %d object(s): %w", name, n, backend.ErrConflict)
	}
	if err := b.project(ctx).DeleteNetworkACL(name); err != nil {
		// An attachment racing the UsedBy pre-check surfaces as the daemon's
		// untyped "Cannot delete an ACL that is in use"; map it like the
		// pre-check would.
		if strings.Contains(err.Error(), "in use") {
			return fmt.Errorf("network ACL %q is in use: %w", name, backend.ErrConflict)
		}
		return fmt.Errorf("delete network ACL %q: %w", name, mapErr(err))
	}
	return nil
}

func toNetworkACL(acl *api.NetworkACL) backend.NetworkACL {
	return backend.NetworkACL{
		Name:        acl.Name,
		Description: acl.Description,
		Ingress:     toRules(acl.Ingress),
		Egress:      toRules(acl.Egress),
		UsedBy:      acl.UsedBy,
	}
}

func toRules(rules []api.NetworkACLRule) []backend.NetworkACLRule {
	out := make([]backend.NetworkACLRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, backend.NetworkACLRule{
			Action:          r.Action,
			Source:          r.Source,
			Destination:     r.Destination,
			Protocol:        r.Protocol,
			SourcePort:      r.SourcePort,
			DestinationPort: r.DestinationPort,
			ICMPType:        r.ICMPType,
			ICMPCode:        r.ICMPCode,
			State:           r.State,
			Description:     r.Description,
		})
	}
	return out
}

func toAPIRules(rules []backend.NetworkACLRule) []api.NetworkACLRule {
	out := make([]api.NetworkACLRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, api.NetworkACLRule{
			Action:          r.Action,
			Source:          r.Source,
			Destination:     r.Destination,
			Protocol:        r.Protocol,
			SourcePort:      r.SourcePort,
			DestinationPort: r.DestinationPort,
			ICMPType:        r.ICMPType,
			ICMPCode:        r.ICMPCode,
			State:           r.State,
			Description:     r.Description,
		})
	}
	return out
}
