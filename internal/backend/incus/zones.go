package incus

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

func (b *incusBackend) ListNetworkZones(ctx context.Context) ([]backend.NetworkZone, error) {
	zones, err := b.project(ctx).GetNetworkZones()
	if err != nil {
		return nil, fmt.Errorf("list network zones: %w", mapErr(err))
	}
	out := make([]backend.NetworkZone, 0, len(zones))
	for i := range zones {
		out = append(out, toNetworkZone(&zones[i], ""))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (b *incusBackend) GetNetworkZone(ctx context.Context, name string) (backend.NetworkZone, error) {
	z, etag, err := b.project(ctx).GetNetworkZone(name)
	if err != nil {
		return backend.NetworkZone{}, fmt.Errorf("get network zone %q: %w", name, mapErr(err))
	}
	return toNetworkZone(z, etag), nil
}

func (b *incusBackend) CreateNetworkZone(ctx context.Context, name, description string) error {
	post := api.NetworkZonesPost{}
	post.Name = name
	post.Description = description
	if err := b.project(ctx).CreateNetworkZone(post); err != nil {
		// The daemon reports a duplicate as a plain 400, which mapErr's typed
		// BadRequest branch would turn into ErrInvalid before the string
		// fallback can see it.
		if strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("network zone %q already exists: %w", name, backend.ErrConflict)
		}
		return fmt.Errorf("create network zone %q: %w", name, mapErr(err))
	}
	return nil
}

// UpdateNetworkZone replaces the zone's description and config. The version
// is the GetNetworkZone etag (412 → ErrConflict); an empty version updates
// unconditionally. NetworkZonePut has no other fields, so there is nothing
// to GET-preserve.
func (b *incusBackend) UpdateNetworkZone(ctx context.Context, name, description string, config map[string]string, version string) error {
	put := api.NetworkZonePut{Description: description, Config: config}
	if err := b.project(ctx).UpdateNetworkZone(name, put, version); err != nil {
		return fmt.Errorf("update network zone %q: %w", name, mapErr(err))
	}
	return nil
}

// DeleteNetworkZone pre-checks UsedBy so a zone referenced by a network
// conflicts cleanly (the daemon's in-use error is an untyped 400).
func (b *incusBackend) DeleteNetworkZone(ctx context.Context, name string) error {
	z, _, err := b.project(ctx).GetNetworkZone(name)
	if err != nil {
		return fmt.Errorf("get network zone %q: %w", name, mapErr(err))
	}
	if n := len(z.UsedBy); n > 0 {
		return fmt.Errorf("network zone %q is in use by %d object(s): %w", name, n, backend.ErrConflict)
	}
	if err := b.project(ctx).DeleteNetworkZone(name); err != nil {
		// A reference racing the UsedBy pre-check surfaces as the daemon's
		// untyped in-use error; map it like the pre-check would.
		if strings.Contains(err.Error(), "in use") {
			return fmt.Errorf("network zone %q is in use: %w", name, backend.ErrConflict)
		}
		return fmt.Errorf("delete network zone %q: %w", name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) ListZoneRecords(ctx context.Context, zone string) ([]backend.ZoneRecord, error) {
	records, err := b.project(ctx).GetNetworkZoneRecords(zone)
	if err != nil {
		return nil, fmt.Errorf("list records of zone %q: %w", zone, mapErr(err))
	}
	out := make([]backend.ZoneRecord, 0, len(records))
	for _, r := range records {
		rec := backend.ZoneRecord{Name: r.Name, Description: r.Description}
		for _, e := range r.Entries {
			rec.Entries = append(rec.Entries, backend.ZoneEntry{Type: e.Type, TTL: e.TTL, Value: e.Value})
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (b *incusBackend) CreateZoneRecord(ctx context.Context, zone string, r backend.ZoneRecord) error {
	post := api.NetworkZoneRecordsPost{}
	post.Name = r.Name
	post.Description = r.Description
	for _, e := range r.Entries {
		post.Entries = append(post.Entries, api.NetworkZoneRecordEntry{Type: e.Type, TTL: e.TTL, Value: e.Value})
	}
	if err := b.project(ctx).CreateNetworkZoneRecord(zone, post); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("zone record %q already exists: %w", r.Name, backend.ErrConflict)
		}
		return fmt.Errorf("create record %q in zone %q: %w", r.Name, zone, mapErr(err))
	}
	return nil
}

func (b *incusBackend) DeleteZoneRecord(ctx context.Context, zone, name string) error {
	if err := b.project(ctx).DeleteNetworkZoneRecord(zone, name); err != nil {
		return fmt.Errorf("delete record %q from zone %q: %w", name, zone, mapErr(err))
	}
	return nil
}

func toNetworkZone(z *api.NetworkZone, etag string) backend.NetworkZone {
	return backend.NetworkZone{
		Name:        z.Name,
		Description: z.Description,
		Config:      z.Config,
		UsedBy:      z.UsedBy,
		Version:     etag,
	}
}
