package incus

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
)

func (b *incusBackend) ListStoragePools(_ context.Context) ([]backend.StoragePool, error) {
	ps, err := b.srv.GetStoragePools()
	if err != nil {
		return nil, fmt.Errorf("list storage pools: %w", mapErr(err))
	}
	out := make([]backend.StoragePool, 0, len(ps))
	for i := range ps {
		out = append(out, toPool(&ps[i]))
	}
	return out, nil
}

func (b *incusBackend) GetStoragePool(_ context.Context, pool string) (backend.StoragePool, error) {
	p, etag, err := b.srv.GetStoragePool(pool)
	if err != nil {
		return backend.StoragePool{}, fmt.Errorf("get storage pool %q: %w", pool, mapErr(err))
	}
	out := toPool(p)
	out.Version = etag
	return out, nil
}

// UpdateStoragePool updates the pool's description and replaces its config via
// GET-preserve-PUT. The version is the etag from GetStoragePool; the daemon
// rejects the PUT with 412 (mapped to ErrConflict) when the pool changed since
// that read. An empty version updates unconditionally. Immutable config keys
// (driver-specific, e.g. zfs.pool_name) are rejected by the daemon with a 400.
func (b *incusBackend) UpdateStoragePool(_ context.Context, name, description string, config map[string]string, version string) error {
	p, _, err := b.srv.GetStoragePool(name)
	if err != nil {
		return fmt.Errorf("get storage pool %q: %w", name, mapErr(err))
	}
	put := p.Writable()
	put.Description = description
	put.Config = config
	if err := b.srv.UpdateStoragePool(name, put, version); err != nil {
		return fmt.Errorf("update storage pool %q: %w", name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) CreateStoragePool(_ context.Context, p backend.StoragePool) error {
	post := api.StoragePoolsPost{Name: p.Name, Driver: p.Driver}
	post.Description = p.Description
	post.Config = p.Config
	if err := b.srv.CreateStoragePool(post); err != nil {
		return fmt.Errorf("create storage pool %q: %w", p.Name, mapErr(err))
	}
	return nil
}

// DeleteStoragePool pre-checks UsedBy (profiles count too) so a referenced
// pool conflicts cleanly; a reference appearing in the stat-then-delete window
// surfaces as the daemon's own 400, which is acceptable.
func (b *incusBackend) DeleteStoragePool(_ context.Context, name string) error {
	p, _, err := b.srv.GetStoragePool(name)
	if err != nil {
		return fmt.Errorf("delete storage pool %q: %w", name, mapErr(err))
	}
	if len(p.UsedBy) > 0 {
		return fmt.Errorf("delete storage pool %q: in use by %s: %w", name, strings.Join(p.UsedBy, ", "), backend.ErrConflict)
	}
	if err := b.srv.DeleteStoragePool(name); err != nil {
		return fmt.Errorf("delete storage pool %q: %w", name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) ListVolumes(_ context.Context, pool string) ([]backend.StorageVolume, error) {
	vs, err := b.srv.GetStoragePoolVolumes(pool)
	if err != nil {
		return nil, fmt.Errorf("list volumes in %q: %w", pool, mapErr(err))
	}
	out := make([]backend.StorageVolume, 0)
	for i := range vs {
		if vs[i].Type == "custom" {
			out = append(out, toVolume(pool, &vs[i]))
		}
	}
	return out, nil
}

func (b *incusBackend) GetVolume(_ context.Context, pool, name string) (backend.StorageVolume, error) {
	v, _, err := b.srv.GetStoragePoolVolume(pool, "custom", name)
	if err != nil {
		return backend.StorageVolume{}, fmt.Errorf("get volume %q/%q: %w", pool, name, mapErr(err))
	}
	return toVolume(pool, v), nil
}

func (b *incusBackend) CreateVolume(_ context.Context, pool string, v backend.StorageVolume) error {
	post := api.StorageVolumesPost{Name: v.Name, Type: "custom", ContentType: v.ContentType}
	post.Config = v.Config
	if err := b.srv.CreateStoragePoolVolume(pool, post); err != nil {
		return fmt.Errorf("create volume %q/%q: %w", pool, v.Name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) DeleteVolume(_ context.Context, pool, name string) error {
	if err := b.srv.DeleteStoragePoolVolume(pool, "custom", name); err != nil {
		return fmt.Errorf("delete volume %q/%q: %w", pool, name, mapErr(err))
	}
	return nil
}

func toPool(p *api.StoragePool) backend.StoragePool {
	return backend.StoragePool{Name: p.Name, Driver: p.Driver, Description: p.Description, Config: p.Config, UsedBy: p.UsedBy}
}

func toVolume(pool string, v *api.StorageVolume) backend.StorageVolume {
	return backend.StorageVolume{Name: v.Name, Type: v.Type, ContentType: v.ContentType, Pool: pool, Config: v.Config, UsedBy: v.UsedBy}
}

func (b *incusBackend) ListVolumeSnapshots(_ context.Context, pool, volume string) ([]backend.StorageVolumeSnapshot, error) {
	ss, err := b.srv.GetStoragePoolVolumeSnapshots(pool, "custom", volume)
	if err != nil {
		return nil, fmt.Errorf("list snapshots %q/%q: %w", pool, volume, mapErr(err))
	}
	out := make([]backend.StorageVolumeSnapshot, 0, len(ss))
	for i := range ss {
		out = append(out, toVolumeSnapshot(&ss[i]))
	}
	return out, nil
}

func (b *incusBackend) CreateVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error {
	op, err := b.srv.CreateStoragePoolVolumeSnapshot(pool, "custom", volume, api.StorageVolumeSnapshotsPost{Name: snapshot})
	return waitOp(ctx, op, err, "snapshot volume %q/%q", pool, volume)
}

func (b *incusBackend) DeleteVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error {
	op, err := b.srv.DeleteStoragePoolVolumeSnapshot(pool, "custom", volume, snapshot)
	return waitOp(ctx, op, err, "delete snapshot %q/%q/%q", pool, volume, snapshot)
}

// RenameVolumeSnapshot renames a custom-volume snapshot (an async operation).
// The target name is pre-checked so a collision is a deterministic ErrConflict:
// a dir-backed daemon can reject the rename with a backend-specific string
// ("file exists", a DB constraint error) that mapErr would not recognize as a
// conflict, surfacing as a 500.
func (b *incusBackend) RenameVolumeSnapshot(ctx context.Context, pool, volume, snapshot, newName string) error {
	snaps, err := b.ListVolumeSnapshots(ctx, pool, volume)
	if err != nil {
		return err
	}
	for _, s := range snaps {
		if s.Name == newName {
			return fmt.Errorf("snapshot %q already exists: %w", newName, backend.ErrConflict)
		}
	}
	op, err := b.srv.RenameStoragePoolVolumeSnapshot(pool, "custom", volume, snapshot, api.StorageVolumeSnapshotPost{Name: newName})
	return waitOp(ctx, op, err, "rename snapshot %q/%q/%q", pool, volume, snapshot)
}

// UpdateVolumeSnapshotExpiry does a GET-preserve-PUT setting ExpiresAt; a zero
// time clears it (nil pointer). UpdateStoragePoolVolumeSnapshot is synchronous.
func (b *incusBackend) UpdateVolumeSnapshotExpiry(_ context.Context, pool, volume, snapshot string, expiresAt time.Time) error {
	s, etag, err := b.srv.GetStoragePoolVolumeSnapshot(pool, "custom", volume, snapshot)
	if err != nil {
		return fmt.Errorf("get snapshot %q/%q/%q: %w", pool, volume, snapshot, mapErr(err))
	}
	put := s.Writable()
	if expiresAt.IsZero() {
		put.ExpiresAt = nil
	} else {
		put.ExpiresAt = &expiresAt
	}
	if err := b.srv.UpdateStoragePoolVolumeSnapshot(pool, "custom", volume, snapshot, put, etag); err != nil {
		return fmt.Errorf("update snapshot expiry %q/%q/%q: %w", pool, volume, snapshot, mapErr(err))
	}
	return nil
}

// RestoreVolumeSnapshot does a GET-then-PUT setting put.Restore.
// UpdateStoragePoolVolume is synchronous (no operation to wait on).
func (b *incusBackend) RestoreVolumeSnapshot(_ context.Context, pool, volume, snapshot string) error {
	v, etag, err := b.srv.GetStoragePoolVolume(pool, "custom", volume)
	if err != nil {
		return fmt.Errorf("get volume %q/%q: %w", pool, volume, mapErr(err))
	}
	put := v.Writable()
	put.Restore = snapshot
	if err := b.srv.UpdateStoragePoolVolume(pool, "custom", volume, put, etag); err != nil {
		return fmt.Errorf("restore volume %q/%q@%q: %w", pool, volume, snapshot, mapErr(err))
	}
	return nil
}

func toVolumeSnapshot(s *api.StorageVolumeSnapshot) backend.StorageVolumeSnapshot {
	// Incus reports volume snapshot names as "<volume>/<snapshot>"; the UI and
	// restore/delete ops use the bare snapshot name (matches ListSnapshots).
	out := backend.StorageVolumeSnapshot{Name: snapshotShortName(s.Name), CreatedAt: s.CreatedAt}
	if s.ExpiresAt != nil {
		out.ExpiresAt = *s.ExpiresAt
	}
	return out
}
