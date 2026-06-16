package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/adam/lxcon/internal/backend"
)

// storedVolumeBackup is one server-side volume backup: the metadata plus the
// blob ExportVolume would have produced at creation time, so download and
// restore round-trip through the same format as streamed export/import.
type storedVolumeBackup struct {
	backend.VolumeBackup

	blob []byte
}

func (f *Fake) ListVolumeBackups(ctx context.Context, pool, volume string) ([]backend.VolumeBackup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(ctx, pool, volume)
	if err != nil {
		return nil, err
	}
	out := make([]backend.VolumeBackup, 0, len(v.backups))
	for _, b := range v.backups {
		out = append(out, b.VolumeBackup)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (f *Fake) CreateVolumeBackup(ctx context.Context, pool, volume, name string, expiresAt time.Time, volumeOnly bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(ctx, pool, volume)
	if err != nil {
		return err
	}
	if name == "" {
		// Daemon parity: backupN, first free index.
		for i := 0; ; i++ {
			if candidate := fmt.Sprintf("backup%d", i); v.backups[candidate] == nil {
				name = candidate
				break
			}
		}
	}
	if v.backups[name] != nil {
		return conflict("backup %q already exists", name)
	}
	// Synthesize the export blob now, so download and restore round-trip
	// through the same format ExportVolume/ImportVolume use.
	payload, err := json.Marshal(volumeBackupBlob{Description: v.Description, Config: v.Config})
	if err != nil {
		return err
	}
	if v.backups == nil {
		v.backups = map[string]*storedVolumeBackup{}
	}
	v.backups[name] = &storedVolumeBackup{
		VolumeBackup: backend.VolumeBackup{Name: name, CreatedAt: f.now(), ExpiresAt: expiresAt, VolumeOnly: volumeOnly},
		blob:         []byte(fakeVolumeBackupMagic + string(payload)),
	}
	f.logOp(f.space(ctx), fmt.Sprintf("Backing up volume %q/%q (%s)", pool, volume, name))
	return nil
}

func (f *Fake) DeleteVolumeBackup(ctx context.Context, pool, volume, backup string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, err := f.lookupVolume(ctx, pool, volume)
	if err != nil {
		return err
	}
	if v.backups[backup] == nil {
		return notFoundf("backup %q", backup)
	}
	delete(v.backups, backup)
	f.logOp(f.space(ctx), fmt.Sprintf("Deleting backup %q of %q/%q", backup, pool, volume))
	return nil
}

func (f *Fake) ExportVolumeBackup(ctx context.Context, pool, volume, backup string, w io.Writer) error {
	f.mu.Lock()
	blob, err := f.lookupVolumeBackupBlob(ctx, pool, volume, backup)
	f.mu.Unlock()
	if err != nil {
		return err
	}
	_, err = w.Write(blob)
	return err
}

// RestoreVolumeBackup feeds the stored blob through the regular import path, so
// restore honors exactly the streamed-import semantics.
func (f *Fake) RestoreVolumeBackup(ctx context.Context, pool, volume, backup, targetPool, newName string) error {
	f.mu.Lock()
	blob, err := f.lookupVolumeBackupBlob(ctx, pool, volume, backup)
	f.mu.Unlock()
	if err != nil {
		return err
	}
	return f.ImportVolume(ctx, targetPool, newName, strings.NewReader(string(blob)))
}

// lookupVolumeBackupBlob returns a copy of a stored backup's blob. Callers must
// hold the mutex.
func (f *Fake) lookupVolumeBackupBlob(ctx context.Context, pool, volume, backup string) ([]byte, error) {
	v, err := f.lookupVolume(ctx, pool, volume)
	if err != nil {
		return nil, err
	}
	bk := v.backups[backup]
	if bk == nil {
		return nil, notFoundf("backup %q", backup)
	}
	return append([]byte(nil), bk.blob...), nil
}
