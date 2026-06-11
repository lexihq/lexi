package fake

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adam/lxcon/internal/backend"
)

func (f *Fake) ListNetworkACLs(_ context.Context) ([]backend.NetworkACL, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]backend.NetworkACL, 0, len(f.acls))
	for name := range f.acls {
		out = append(out, f.aclView(name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetNetworkACL(_ context.Context, name string) (backend.NetworkACL, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.acls[name]; !ok {
		return backend.NetworkACL{}, notFoundf("network ACL %q", name)
	}
	acl := f.aclView(name)
	acl.Version = strconv.Itoa(f.aclVersions[name])
	return acl, nil
}

func (f *Fake) CreateNetworkACL(_ context.Context, name, description string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !validACLName(name) {
		return invalid("invalid network ACL name %q", name)
	}
	if _, ok := f.acls[name]; ok {
		return conflict("network ACL %q already exists", name)
	}
	f.acls[name] = backend.NetworkACL{Name: name, Description: description}
	return nil
}

func (f *Fake) UpdateNetworkACL(_ context.Context, name, description string, ingress, egress []backend.NetworkACLRule, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	acl, ok := f.acls[name]
	if !ok {
		return notFoundf("network ACL %q", name)
	}
	// Empty version = unconditional, mirroring the Incus client's If-Match
	// semantics; a stale version means a concurrent writer landed first.
	if version != "" && version != strconv.Itoa(f.aclVersions[name]) {
		return conflict("network ACL %q version %s", name, version)
	}
	acl.Description = description
	acl.Ingress = append([]backend.NetworkACLRule(nil), ingress...)
	acl.Egress = append([]backend.NetworkACLRule(nil), egress...)
	f.acls[name] = acl
	f.aclVersions[name]++
	return nil
}

func (f *Fake) RenameNetworkACL(_ context.Context, name, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	acl, ok := f.acls[name]
	if !ok {
		return notFoundf("network ACL %q", name)
	}
	// Incus parity: the daemon refuses renaming an attached ACL.
	if used := f.aclUsedBy(name); len(used) > 0 {
		return conflict("network ACL %q is in use", name)
	}
	if !validACLName(newName) {
		return invalid("invalid network ACL name %q", newName)
	}
	if _, exists := f.acls[newName]; exists {
		return conflict("network ACL %q already exists", newName)
	}
	acl.Name = newName
	f.acls[newName] = acl
	f.aclVersions[newName] = f.aclVersions[name] + 1
	delete(f.acls, name)
	delete(f.aclVersions, name)
	return nil
}

func (f *Fake) DeleteNetworkACL(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.acls[name]; !ok {
		return notFoundf("network ACL %q", name)
	}
	if used := f.aclUsedBy(name); len(used) > 0 {
		return conflict("network ACL %q is in use", name)
	}
	delete(f.acls, name)
	delete(f.aclVersions, name)
	return nil
}

// aclView materializes an ACL with a fresh UsedBy. Callers must hold the mutex.
func (f *Fake) aclView(name string) backend.NetworkACL {
	acl := f.acls[name]
	acl.Ingress = append([]backend.NetworkACLRule(nil), acl.Ingress...)
	acl.Egress = append([]backend.NetworkACLRule(nil), acl.Egress...)
	acl.UsedBy = f.aclUsedBy(name)
	return acl
}

// aclUsedBy lists API paths of networks, instances, and profiles referencing
// the ACL via security.acls (network config or NIC device config), mirroring
// the daemon's UsedBy: instances are scanned with profile-expanded devices,
// only nics bound to a network count, and every matching NIC appends its
// owner's path (no dedup). Callers must hold the mutex.
func (f *Fake) aclUsedBy(name string) []string {
	var used []string
	for netName, n := range f.networks {
		if slices.Contains(splitCommaList(n.Config["security.acls"]), name) {
			used = append(used, "/1.0/networks/"+netName)
		}
	}
	for instName, inst := range f.instances {
		for range aclNICMatches(f.expandedDevices(inst), name) {
			used = append(used, "/1.0/instances/"+instName)
		}
	}
	for profName, p := range f.profiles {
		for range aclNICMatches(p.Devices, name) {
			used = append(used, "/1.0/profiles/"+profName)
		}
	}
	sort.Strings(used)
	return used
}

// expandedDevices merges the instance's profile devices (in profile order)
// under its local devices (locals shadow by device name), mirroring the
// daemon's ExpandInstanceDevices. Callers must hold the mutex.
func (f *Fake) expandedDevices(inst *instance) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, profName := range inst.Profiles {
		maps.Copy(out, f.profiles[profName].Devices)
	}
	maps.Copy(out, inst.devices)
	return out
}

// aclNICMatches lists the devices that count as ACL usage: nics bound to a
// network whose security.acls lists the ACL (daemon's isInUseByDevice).
func aclNICMatches(devices map[string]map[string]string, name string) []string {
	var matched []string
	for dn, dev := range devices {
		if dev["type"] != "nic" || dev["network"] == "" {
			continue
		}
		if slices.Contains(splitCommaList(dev["security.acls"]), name) {
			matched = append(matched, dn)
		}
	}
	return matched
}

// validACLName mirrors the daemon's acl.ValidName: an API name that does not
// start with the reserved port-selector characters and is hostname-shaped (so
// rules can tell an ACL reference from an IP).
func validACLName(name string) bool {
	if !validAPIName(name) {
		return false
	}
	if strings.HasPrefix(name, "@") || strings.HasPrefix(name, "%") || strings.HasPrefix(name, "#") {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return !strings.HasPrefix(name, "-") && !strings.HasSuffix(name, "-")
}
