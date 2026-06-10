package fake

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

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

func (f *Fake) CreateStoragePool(_ context.Context, p backend.StoragePool) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Driver) == "" {
		return invalid("storage pool name and driver are required")
	}
	// Incus parity: reject names the daemon refuses so fake-backed tests
	// can't pass with names production rejects (same as fake profile names).
	if strings.ContainsAny(p.Name, " \t\n/") || len(p.Name) > 64 {
		return invalid("storage pool name %q is not a valid pool name", p.Name)
	}
	if !slices.Contains([]string{"dir", "zfs", "btrfs", "lvm"}, p.Driver) {
		return invalid("storage pool driver %q is not supported", p.Driver)
	}
	if _, ok := f.pools[p.Name]; ok {
		return conflict("storage pool %q already exists", p.Name)
	}
	f.pools[p.Name] = &storagePool{
		StoragePool: backend.StoragePool{Name: p.Name, Driver: p.Driver, Description: p.Description, Config: maps.Clone(p.Config)},
		volumes:     map[string]*storageVolume{},
	}
	return nil
}

func (f *Fake) DeleteStoragePool(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pools[name]
	if !ok {
		return notFoundf("storage pool %q", name)
	}
	if used := f.poolUsedBy(p); len(used) > 0 {
		return conflict("storage pool %q is in use by %s", name, strings.Join(used, ", "))
	}
	delete(f.pools, name)
	return nil
}

// poolUsedBy lists the API paths of profiles, instances, and custom volumes
// referencing the pool, mirroring the daemon's UsedBy. Callers must hold the
// mutex.
func (f *Fake) poolUsedBy(p *storagePool) []string {
	var used []string
	for name, prof := range f.profiles {
		for _, dev := range prof.Devices {
			if dev["pool"] == p.Name {
				used = append(used, "/1.0/profiles/"+name)
				break
			}
		}
	}
	for name, in := range f.instances {
		for _, dev := range in.devices {
			if dev["pool"] == p.Name {
				used = append(used, "/1.0/instances/"+name)
				break
			}
		}
	}
	for name := range p.volumes {
		used = append(used, "/1.0/storage-pools/"+p.Name+"/volumes/custom/"+name)
	}
	sort.Strings(used)
	return used
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

// poolView returns a copy with a cloned config and a freshly computed UsedBy.
// Callers must hold the mutex.
func (f *Fake) poolView(p *storagePool) backend.StoragePool {
	out := p.StoragePool
	out.Config = maps.Clone(p.Config)
	out.UsedBy = f.poolUsedBy(p)
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

func (f *Fake) ListVolumeSnapshots(_ context.Context, pool, volume string) ([]backend.StorageVolumeSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(pool, volume)
	if err != nil {
		return nil, err
	}
	out := append([]backend.StorageVolumeSnapshot(nil), v.snapshots...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) CreateVolumeSnapshot(_ context.Context, pool, volume, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(pool, volume)
	if err != nil {
		return err
	}
	for _, s := range v.snapshots {
		if s.Name == snapshot {
			return conflict("snapshot %q already exists", snapshot)
		}
	}
	v.snapshots = append(v.snapshots, backend.StorageVolumeSnapshot{Name: snapshot, CreatedAt: f.now()})
	return nil
}

func (f *Fake) RestoreVolumeSnapshot(_ context.Context, pool, volume, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(pool, volume)
	if err != nil {
		return err
	}
	if !hasSnapshot(v.snapshots, snapshot) {
		return notFoundf("snapshot %q", snapshot)
	}
	return nil
}

func (f *Fake) RenameVolumeSnapshot(_ context.Context, pool, volume, snapshot, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(pool, volume)
	if err != nil {
		return err
	}
	if hasSnapshot(v.snapshots, newName) {
		return conflict("snapshot %q already exists", newName)
	}
	for i := range v.snapshots {
		if v.snapshots[i].Name == snapshot {
			v.snapshots[i].Name = newName
			return nil
		}
	}
	return notFoundf("snapshot %q", snapshot)
}

func (f *Fake) UpdateVolumeSnapshotExpiry(_ context.Context, pool, volume, snapshot string, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(pool, volume)
	if err != nil {
		return err
	}
	for i := range v.snapshots {
		if v.snapshots[i].Name == snapshot {
			v.snapshots[i].ExpiresAt = expiresAt
			return nil
		}
	}
	return notFoundf("snapshot %q", snapshot)
}

func (f *Fake) DeleteVolumeSnapshot(_ context.Context, pool, volume, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(pool, volume)
	if err != nil {
		return err
	}
	for i, s := range v.snapshots {
		if s.Name == snapshot {
			v.snapshots = append(v.snapshots[:i], v.snapshots[i+1:]...)
			return nil
		}
	}
	return notFoundf("snapshot %q", snapshot)
}

func hasSnapshot(snaps []backend.StorageVolumeSnapshot, name string) bool {
	for _, s := range snaps {
		if s.Name == name {
			return true
		}
	}
	return false
}
