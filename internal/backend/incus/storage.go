package incus

import (
	"context"
	"fmt"

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
	p, _, err := b.srv.GetStoragePool(pool)
	if err != nil {
		return backend.StoragePool{}, fmt.Errorf("get storage pool %q: %w", pool, mapErr(err))
	}
	return toPool(p), nil
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
