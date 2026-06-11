package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adam/lxcon/internal/backend"
)

func (f *Fake) ListStoragePools(ctx context.Context) ([]backend.StoragePool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]backend.StoragePool, 0, len(f.pools))
	for _, p := range f.pools {
		out = append(out, f.poolView(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetStoragePool(ctx context.Context, pool string) (backend.StoragePool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pools[pool]
	if !ok {
		return backend.StoragePool{}, notFoundf("storage pool %q", pool)
	}
	out := f.poolView(p)
	out.Version = strconv.Itoa(p.version)
	return out, nil
}

func (f *Fake) UpdateStoragePool(ctx context.Context, name, description string, config map[string]string, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pools[name]
	if !ok {
		return notFoundf("storage pool %q", name)
	}
	// Empty version = unconditional, mirroring the Incus client's If-Match
	// semantics; a stale version means a concurrent writer landed first.
	if version != "" && version != strconv.Itoa(p.version) {
		return conflict("storage pool %q version %s", name, version)
	}
	p.Description = description
	p.Config = maps.Clone(config)
	if p.Config == nil {
		p.Config = map[string]string{}
	}
	p.version++
	return nil
}

func (f *Fake) CreateStoragePool(ctx context.Context, p backend.StoragePool) error {
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
		volumes:     map[string]map[string]*storageVolume{},
	}
	return nil
}

func (f *Fake) DeleteStoragePool(ctx context.Context, name string) error {
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
	for _, spc := range f.spaces {
		for name, prof := range spc.profiles {
			for _, dev := range prof.Devices {
				if dev["pool"] == p.Name {
					used = append(used, "/1.0/profiles/"+name)
					break
				}
			}
		}
	}
	for _, spc := range f.spaces {
		for name, in := range spc.instances {
			for _, dev := range in.devices {
				if dev["pool"] == p.Name {
					used = append(used, "/1.0/instances/"+name)
					break
				}
			}
		}
	}
	for project := range p.volumes {
		for name := range p.volumes[project] {
			used = append(used, "/1.0/storage-pools/"+p.Name+"/volumes/custom/"+name)
		}
	}
	sort.Strings(used)
	return used
}

func (f *Fake) ListVolumes(ctx context.Context, pool string) ([]backend.StorageVolume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pools[pool]
	if !ok {
		return nil, notFoundf("storage pool %q", pool)
	}
	out := make([]backend.StorageVolume, 0, len(p.vols(f.featureProject(ctx, "features.storage.volumes"))))
	for _, v := range p.vols(f.featureProject(ctx, "features.storage.volumes")) {
		out = append(out, volumeView(v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) GetVolume(ctx context.Context, pool, name string) (backend.StorageVolume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(ctx, pool, name)
	if err != nil {
		return backend.StorageVolume{}, err
	}
	out := volumeView(v)
	out.Version = strconv.Itoa(v.version)
	return out, nil
}

func (f *Fake) UpdateVolume(ctx context.Context, pool, name, description string, config map[string]string, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(ctx, pool, name)
	if err != nil {
		return err
	}
	// Empty version = unconditional, mirroring the Incus client's If-Match
	// semantics; a stale version means a concurrent writer landed first.
	if version != "" && version != strconv.Itoa(v.version) {
		return conflict("volume %q version %s", name, version)
	}
	v.Description = description
	v.Config = maps.Clone(config)
	if v.Config == nil {
		v.Config = map[string]string{}
	}
	v.version++
	return nil
}

func (f *Fake) RenameVolume(ctx context.Context, pool, name, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pools[pool]
	if !ok {
		return notFoundf("storage pool %q", pool)
	}
	v, ok := p.vols(f.featureProject(ctx, "features.storage.volumes"))[name]
	if !ok {
		return notFoundf("volume %q", name)
	}
	// Incus parity: volume names are API names (no whitespace or separators).
	if !validAPIName(newName) {
		return invalid("invalid volume name %q", newName)
	}
	if _, exists := p.vols(f.featureProject(ctx, "features.storage.volumes"))[newName]; exists {
		return conflict("volume %q already exists", newName)
	}
	// Incus parity: the daemon refuses renaming a volume a running instance
	// has attached; stopped instances' disk-device references follow the
	// rename.
	// Only projects sharing this volume namespace can reference the volume;
	// same-named volumes in isolated projects must not block or be rewritten.
	owner := f.featureProject(ctx, "features.storage.volumes")
	for project, spc := range f.spaces {
		if f.featureProjectName(project, "features.storage.volumes") != owner {
			continue
		}
		for instName, in := range spc.instances {
			for _, dev := range in.devices {
				if dev["pool"] == pool && dev["source"] == name {
					if in.Status == "Running" {
						return invalid("volume %q is in use by running instance %q", name, instName)
					}
				}
			}
		}
	}
	for project, spc := range f.spaces {
		if f.featureProjectName(project, "features.storage.volumes") != owner {
			continue
		}
		for _, in := range spc.instances {
			for _, dev := range in.devices {
				if dev["pool"] == pool && dev["source"] == name {
					dev["source"] = newName
				}
			}
		}
	}
	v.Name = newName
	p.vols(f.featureProject(ctx, "features.storage.volumes"))[newName] = v
	delete(p.vols(f.featureProject(ctx, "features.storage.volumes")), name)
	return nil
}

func (f *Fake) CreateVolume(ctx context.Context, pool string, v backend.StorageVolume) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pools[pool]
	if !ok {
		return notFoundf("storage pool %q", pool)
	}
	if _, ok := p.vols(f.featureProject(ctx, "features.storage.volumes"))[v.Name]; ok {
		return conflict("volume %q already exists", v.Name)
	}
	contentType := v.ContentType
	if contentType == "" {
		contentType = "filesystem"
	}
	p.vols(f.featureProject(ctx, "features.storage.volumes"))[v.Name] = &storageVolume{
		StorageVolume: backend.StorageVolume{
			Name: v.Name, Type: "custom", ContentType: contentType,
			Pool: pool, Description: v.Description, Config: maps.Clone(v.Config),
		},
	}
	return nil
}

func (f *Fake) DeleteVolume(ctx context.Context, pool, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pools[pool]
	if !ok {
		return notFoundf("storage pool %q", pool)
	}
	if _, ok := p.vols(f.featureProject(ctx, "features.storage.volumes"))[name]; !ok {
		return notFoundf("volume %q", name)
	}
	delete(p.vols(f.featureProject(ctx, "features.storage.volumes")), name)
	return nil
}

// fakeVolumeBackupMagic prefixes the blob ExportVolume writes so ImportVolume
// can recognize a lxcon-produced volume backup, mirroring fakeBackupMagic.
const fakeVolumeBackupMagic = "lxcon-fake-volume-backup\n"

// volumeBackupBlob is the JSON payload after the magic: just enough state to
// make the export→import round-trip observable in tests.
type volumeBackupBlob struct {
	Description string            `json:"description"`
	Config      map[string]string `json:"config"`
}

func (f *Fake) ExportVolume(ctx context.Context, pool, volume string, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pools[pool]
	if !ok {
		return notFoundf("storage pool %q", pool)
	}
	v, ok := p.vols(f.featureProject(ctx, "features.storage.volumes"))[volume]
	if !ok {
		return notFoundf("volume %q", volume)
	}
	payload, err := json.Marshal(volumeBackupBlob{Description: v.Description, Config: v.Config})
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, fakeVolumeBackupMagic+string(payload))
	return err
}

func (f *Fake) ImportVolume(ctx context.Context, pool, volume string, r io.Reader) error {
	blob, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	payload, ok := strings.CutPrefix(string(blob), fakeVolumeBackupMagic)
	if !ok {
		return fmt.Errorf("not a lxcon volume backup: %w", backend.ErrInvalid)
	}
	var vb volumeBackupBlob
	if err := json.Unmarshal([]byte(payload), &vb); err != nil {
		return fmt.Errorf("corrupt volume backup: %w", backend.ErrInvalid)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pools[pool]
	if !ok {
		return notFoundf("storage pool %q", pool)
	}
	if _, ok := p.vols(f.featureProject(ctx, "features.storage.volumes"))[volume]; ok {
		return conflict("volume %q already exists", volume)
	}
	p.vols(f.featureProject(ctx, "features.storage.volumes"))[volume] = &storageVolume{
		StorageVolume: backend.StorageVolume{
			Name: volume, Type: "custom", ContentType: "filesystem",
			Pool: pool, Description: vb.Description, Config: maps.Clone(vb.Config),
		},
	}
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
func (f *Fake) lookupVolume(ctx context.Context, pool, name string) (*storageVolume, error) {
	p, ok := f.pools[pool]
	if !ok {
		return nil, notFoundf("storage pool %q", pool)
	}
	v, ok := p.vols(f.featureProject(ctx, "features.storage.volumes"))[name]
	if !ok {
		return nil, notFoundf("volume %q", name)
	}
	return v, nil
}

func (f *Fake) ListVolumeSnapshots(ctx context.Context, pool, volume string) ([]backend.StorageVolumeSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(ctx, pool, volume)
	if err != nil {
		return nil, err
	}
	out := append([]backend.StorageVolumeSnapshot(nil), v.snapshots...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) CreateVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(ctx, pool, volume)
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

func (f *Fake) RestoreVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(ctx, pool, volume)
	if err != nil {
		return err
	}
	if !hasSnapshot(v.snapshots, snapshot) {
		return notFoundf("snapshot %q", snapshot)
	}
	return nil
}

func (f *Fake) RenameVolumeSnapshot(ctx context.Context, pool, volume, snapshot, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(ctx, pool, volume)
	if err != nil {
		return err
	}
	// Incus parity: snapshot names are API names (no whitespace or separators).
	if !validAPIName(newName) {
		return invalid("invalid snapshot name %q", newName)
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

func (f *Fake) UpdateVolumeSnapshotExpiry(ctx context.Context, pool, volume, snapshot string, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(ctx, pool, volume)
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

func (f *Fake) DeleteVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(ctx, pool, volume)
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
