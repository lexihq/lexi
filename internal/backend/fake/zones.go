package fake

import (
	"context"
	"maps"
	"sort"
	"strconv"
	"strings"

	"github.com/adam/lxcon/internal/backend"
)

// zoneSpace returns the space owning the request project's DNS zones (the
// project's own under features.networks.zones, else default's). Callers must
// hold the mutex.
func (f *Fake) zoneSpace(ctx context.Context) *space {
	return f.featureSpace(ctx, "features.networks.zones")
}

func (f *Fake) ListNetworkZones(ctx context.Context) ([]backend.NetworkZone, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.zoneSpace(ctx)

	out := make([]backend.NetworkZone, 0, len(sp.zones))
	for name := range sp.zones {
		out = append(out, f.zoneView(ctx, sp, name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetNetworkZone(ctx context.Context, name string) (backend.NetworkZone, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.zoneSpace(ctx)

	if _, ok := sp.zones[name]; !ok {
		return backend.NetworkZone{}, notFoundf("network zone %q", name)
	}
	z := f.zoneView(ctx, sp, name)
	z.Version = strconv.Itoa(sp.zoneVersions[name])
	return z, nil
}

func (f *Fake) CreateNetworkZone(ctx context.Context, name, description string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.zoneSpace(ctx)

	if !validZoneName(name) {
		return invalid("invalid network zone name %q", name)
	}
	if _, ok := sp.zones[name]; ok {
		return conflict("network zone %q already exists", name)
	}
	sp.zones[name] = backend.NetworkZone{Name: name, Description: description, Config: map[string]string{}}
	return nil
}

func (f *Fake) UpdateNetworkZone(ctx context.Context, name, description string, config map[string]string, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.zoneSpace(ctx)

	z, ok := sp.zones[name]
	if !ok {
		return notFoundf("network zone %q", name)
	}
	// Empty version = unconditional, mirroring the Incus client's If-Match
	// semantics; a stale version means a concurrent writer landed first.
	if version != "" && version != strconv.Itoa(sp.zoneVersions[name]) {
		return conflict("network zone %q version %s", name, version)
	}
	z.Description = description
	z.Config = maps.Clone(config)
	if z.Config == nil {
		z.Config = map[string]string{}
	}
	sp.zones[name] = z
	sp.zoneVersions[name]++
	return nil
}

func (f *Fake) DeleteNetworkZone(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.zoneSpace(ctx)

	if _, ok := sp.zones[name]; !ok {
		return notFoundf("network zone %q", name)
	}
	if used := f.zoneUsedBy(ctx, name); len(used) > 0 {
		return conflict("network zone %q is in use", name)
	}
	delete(sp.zones, name)
	delete(sp.zoneVersions, name)
	delete(sp.zoneRecords, name)
	return nil
}

func (f *Fake) ListZoneRecords(ctx context.Context, zone string) ([]backend.ZoneRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.zoneSpace(ctx)

	if _, ok := sp.zones[zone]; !ok {
		return nil, notFoundf("network zone %q", zone)
	}
	out := make([]backend.ZoneRecord, 0, len(sp.zoneRecords[zone]))
	for _, r := range sp.zoneRecords[zone] {
		r.Entries = append([]backend.ZoneEntry(nil), r.Entries...)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) CreateZoneRecord(ctx context.Context, zone string, r backend.ZoneRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.zoneSpace(ctx)

	if _, ok := sp.zones[zone]; !ok {
		return notFoundf("network zone %q", zone)
	}
	if !validRecordName(r.Name) {
		return invalid("invalid zone record name %q", r.Name)
	}
	for _, e := range r.Entries {
		if e.Type == "" || e.Value == "" {
			return invalid("zone record %q: every entry needs a type and value", r.Name)
		}
	}
	if _, ok := sp.zoneRecords[zone][r.Name]; ok {
		return conflict("zone record %q already exists", r.Name)
	}
	if sp.zoneRecords[zone] == nil {
		sp.zoneRecords[zone] = map[string]backend.ZoneRecord{}
	}
	r.Entries = append([]backend.ZoneEntry(nil), r.Entries...)
	sp.zoneRecords[zone][r.Name] = r
	return nil
}

func (f *Fake) DeleteZoneRecord(ctx context.Context, zone, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.zoneSpace(ctx)

	if _, ok := sp.zones[zone]; !ok {
		return notFoundf("network zone %q", zone)
	}
	if _, ok := sp.zoneRecords[zone][name]; !ok {
		return notFoundf("zone record %q", name)
	}
	delete(sp.zoneRecords[zone], name)
	return nil
}

// zoneView materializes a zone with a fresh UsedBy and cloned config.
// Callers must hold the mutex.
func (f *Fake) zoneView(ctx context.Context, sp *space, name string) backend.NetworkZone {
	z := sp.zones[name]
	z.Config = maps.Clone(z.Config)
	z.UsedBy = f.zoneUsedBy(ctx, name)
	return z
}

// zoneUsedBy lists API paths of networks referencing the zone via their
// dns.zone.* config keys, mirroring the daemon's UsedBy. Networks live in
// their own feature space, which may differ from the zone's. Callers must
// hold the mutex.
func (f *Fake) zoneUsedBy(ctx context.Context, name string) []string {
	var used []string
	for netName, n := range f.networkSpace(ctx).networks {
		for _, key := range []string{"dns.zone.forward", "dns.zone.reverse.ipv4", "dns.zone.reverse.ipv6"} {
			if splitsContain(n.Config[key], name) {
				used = append(used, "/1.0/networks/"+netName)
				break
			}
		}
	}
	sort.Strings(used)
	return used
}

// splitsContain reports whether the comma-separated list contains name.
func splitsContain(list, name string) bool {
	for part := range strings.SplitSeq(list, ",") {
		if strings.TrimSpace(part) == name {
			return true
		}
	}
	return false
}

// validZoneName mirrors the daemon's zone validation closely enough for the
// fake: a dotted DNS name of hostname-shaped labels.
func validZoneName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for label := range strings.SplitSeq(name, ".") {
		if !validDNSLabel(label) {
			return false
		}
	}
	return true
}

// validRecordName accepts relative record names: one or more DNS labels
// (records like "www" or "db.internal").
func validRecordName(name string) bool {
	return validZoneName(name)
}

func validDNSLabel(label string) bool {
	if label == "" || len(label) > 63 {
		return false
	}
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return !strings.HasPrefix(label, "-") && !strings.HasSuffix(label, "-")
}
