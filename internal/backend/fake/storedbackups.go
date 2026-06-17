package fake

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/lexihq/lexi/internal/backend"
)

// storedBackup is one server-side backup: the metadata plus the blob
// ExportInstance would have produced at creation time, so download and
// restore round-trip through the same format as streamed export/import.
type storedBackup struct {
	backend.InstanceBackup

	blob []byte
}

func (f *Fake) ListInstanceBackups(ctx context.Context, instance string) ([]backend.InstanceBackup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[instance]
	if !ok {
		return nil, notFound(instance)
	}
	out := make([]backend.InstanceBackup, 0, len(in.backups))
	for _, b := range in.backups {
		out = append(out, b.InstanceBackup)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (f *Fake) CreateInstanceBackup(ctx context.Context, instance, name string, expiresAt time.Time, instanceOnly bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[instance]
	if !ok {
		return notFound(instance)
	}
	if name == "" {
		// Daemon parity: backupN, first free index.
		for i := 0; ; i++ {
			candidate := fmt.Sprintf("backup%d", i)
			if _, taken := in.backups[candidate]; !taken {
				name = candidate
				break
			}
		}
	}
	if _, taken := in.backups[name]; taken {
		return conflict("backup %q already exists", name)
	}
	if in.backups == nil {
		in.backups = map[string]*storedBackup{}
	}
	in.backups[name] = &storedBackup{
		InstanceBackup: backend.InstanceBackup{
			Name: name, CreatedAt: f.now(), ExpiresAt: expiresAt, InstanceOnly: instanceOnly,
		},
		blob: []byte(fakeBackupMagic + in.Image),
	}
	f.logOp(sp, fmt.Sprintf("Backing up instance %q (%s)", instance, name))
	return nil
}

func (f *Fake) DeleteInstanceBackup(ctx context.Context, instance, backup string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[instance]
	if !ok {
		return notFound(instance)
	}
	if _, ok := in.backups[backup]; !ok {
		return notFoundf("backup %q", backup)
	}
	delete(in.backups, backup)
	f.logOp(sp, fmt.Sprintf("Deleting backup %q of %q", backup, instance))
	return nil
}

func (f *Fake) ExportInstanceBackup(ctx context.Context, instance, backup string, w io.Writer) error {
	f.mu.Lock()
	bk, err := f.lookupBackup(ctx, instance, backup)
	if err != nil {
		f.mu.Unlock()
		return err
	}
	blob := append([]byte(nil), bk.blob...)
	f.mu.Unlock()

	_, err = w.Write(blob)
	return err
}

// RestoreInstanceBackup feeds the stored blob through the regular import
// path, so the restore honors exactly the streamed-import semantics.
func (f *Fake) RestoreInstanceBackup(ctx context.Context, instance, backup, newName string) error {
	f.mu.Lock()
	bk, err := f.lookupBackup(ctx, instance, backup)
	if err != nil {
		f.mu.Unlock()
		return err
	}
	blob := append([]byte(nil), bk.blob...)
	f.mu.Unlock()

	return f.ImportInstance(ctx, newName, strings.NewReader(string(blob)))
}

// lookupBackup resolves a stored backup. Callers must hold the mutex.
func (f *Fake) lookupBackup(ctx context.Context, instance, backup string) (*storedBackup, error) {
	sp := f.space(ctx)
	in, ok := sp.instances[instance]
	if !ok {
		return nil, notFound(instance)
	}
	bk, ok := in.backups[backup]
	if !ok {
		return nil, notFoundf("backup %q", backup)
	}
	return bk, nil
}
