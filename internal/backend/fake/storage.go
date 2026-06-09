package fake

import (
	"context"
	"maps"
	"sort"

	"github.com/adam/lxcon/internal/backend"
)

func (f *Fake) ListStoragePools(_ context.Context) ([]backend.StoragePool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]backend.StoragePool, 0, len(f.pools))
	for _, p := range f.pools {
		out = append(out, f.poolView(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetStoragePool(_ context.Context, pool string) (backend.StoragePool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pools[pool]
	if !ok {
		return backend.StoragePool{}, notFoundf("storage pool %q", pool)
	}
	return f.poolView(p), nil
}

func (f *Fake) ListVolumes(_ context.Context, pool string) ([]backend.StorageVolume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pools[pool]
	if !ok {
		return nil, notFoundf("storage pool %q", pool)
	}
	out := make([]backend.StorageVolume, 0, len(p.volumes))
	for _, v := range p.volumes {
		out = append(out, volumeView(v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetVolume(_ context.Context, pool, name string) (backend.StorageVolume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(pool, name)
	if err != nil {
		return backend.StorageVolume{}, err
	}
	return volumeView(v), nil
}

func (f *Fake) CreateVolume(_ context.Context, pool string, v backend.StorageVolume) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pools[pool]
	if !ok {
		return notFoundf("storage pool %q", pool)
	}
	if _, ok := p.volumes[v.Name]; ok {
		return conflict("volume %q already exists", v.Name)
	}
	contentType := v.ContentType
	if contentType == "" {
		contentType = "filesystem"
	}
	p.volumes[v.Name] = &storageVolume{
		StorageVolume: backend.StorageVolume{
			Name: v.Name, Type: "custom", ContentType: contentType,
			Pool: pool, Config: maps.Clone(v.Config),
		},
	}
	return nil
}

func (f *Fake) DeleteVolume(_ context.Context, pool, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pools[pool]
	if !ok {
		return notFoundf("storage pool %q", pool)
	}
	if _, ok := p.volumes[name]; !ok {
		return notFoundf("volume %q", name)
	}
	delete(p.volumes, name)
	return nil
}

// poolView returns a copy with a cloned config. Callers must hold the mutex.
func (f *Fake) poolView(p *storagePool) backend.StoragePool {
	out := p.StoragePool
	out.Config = maps.Clone(p.Config)
	return out
}

// volumeView returns a copy with a cloned config. Callers must hold the mutex.
func volumeView(v *storageVolume) backend.StorageVolume {
	out := v.StorageVolume
	out.Config = maps.Clone(v.Config)
	return out
}

// lookupVolume resolves a pool+volume, returning a not-found error at the right
// level. Callers must hold the mutex.
func (f *Fake) lookupVolume(pool, name string) (*storageVolume, error) {
	p, ok := f.pools[pool]
	if !ok {
		return nil, notFoundf("storage pool %q", pool)
	}
	v, ok := p.volumes[name]
	if !ok {
		return nil, notFoundf("volume %q", name)
	}
	return v, nil
}
