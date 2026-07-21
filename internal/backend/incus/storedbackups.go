package incus

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/lxc/incus/v6/shared/cancel"

	"github.com/lexihq/lexi/internal/backend"
)

// ListInstanceBackups lists the named backups stored on the server, oldest
// first (the daemon already returns them in creation order).
func (b *incusBackend) ListInstanceBackups(ctx context.Context, instance string) ([]backend.InstanceBackup, error) {
	bks, err := b.project(ctx).GetInstanceBackups(instance)
	if err != nil {
		return nil, fmt.Errorf("list backups of %q: %w", instance, mapErr(err))
	}
	out := make([]backend.InstanceBackup, 0, len(bks))
	for _, bk := range bks {
		out = append(out, backend.InstanceBackup{
			Name:         bk.Name,
			CreatedAt:    bk.CreatedAt,
			ExpiresAt:    bk.ExpiresAt,
			InstanceOnly: bk.InstanceOnly,
		})
	}
	return out, nil
}

// CreateInstanceBackup stores a named server-side backup. The daemon has no
// name default, so an empty name gets the CLI's backupN convention (first
// free index). A non-zero expiry needs the backup_expiry extension.
func (b *incusBackend) CreateInstanceBackup(ctx context.Context, instance, name string, expiresAt time.Time, instanceOnly bool) error {
	srv := b.project(ctx)
	if !expiresAt.IsZero() && !b.server(ctx).HasExtension("backup_expiry") {
		return fmt.Errorf("backup expiry needs the daemon's backup_expiry extension: %w", backend.ErrUnsupported)
	}
	if name == "" {
		existing, err := srv.GetInstanceBackups(instance)
		if err != nil {
			return fmt.Errorf("list backups of %q: %w", instance, mapErr(err))
		}
		taken := make(map[string]bool, len(existing))
		for _, bk := range existing {
			taken[bk.Name] = true
		}
		for i := 0; ; i++ {
			if candidate := fmt.Sprintf("backup%d", i); !taken[candidate] {
				name = candidate
				break
			}
		}
	}
	op, err := srv.CreateInstanceBackup(instance, api.InstanceBackupsPost{
		Name:                 name,
		ExpiresAt:            expiresAt,
		InstanceOnly:         instanceOnly,
		CompressionAlgorithm: "gzip",
	})
	return waitOp(ctx, op, err, "create backup %q of %q", name, instance)
}

func (b *incusBackend) DeleteInstanceBackup(ctx context.Context, instance, backup string) error {
	op, err := b.project(ctx).DeleteInstanceBackup(instance, backup)
	return waitOp(ctx, op, err, "delete backup %q of %q", backup, instance)
}

// ExportInstanceBackup spools the stored backup to a temp file (the client
// needs an io.WriteSeeker) and streams it to w. Unlike ExportInstance, the
// backup is persistent — nothing is created or cleaned up server-side.
func (b *incusBackend) ExportInstanceBackup(ctx context.Context, instance, backup string, w io.Writer) error {
	tmp, err := b.spoolBackup(ctx, instance, backup)
	if err != nil {
		return err
	}
	defer cleanupExportTemp(tmp)
	if _, err := io.Copy(w, tmp); err != nil {
		return fmt.Errorf("stream backup %q of %q: %w", backup, instance, err)
	}
	return nil
}

// RestoreInstanceBackup creates a new instance from a stored backup. The
// tarball round-trips through a temp spool because the download needs a
// WriteSeeker; nothing crosses the browser.
func (b *incusBackend) RestoreInstanceBackup(ctx context.Context, instance, backup, newName string) error {
	tmp, err := b.spoolBackup(ctx, instance, backup)
	if err != nil {
		return err
	}
	defer cleanupExportTemp(tmp)

	op, err := b.project(ctx).CreateInstanceFromBackup(incusclient.InstanceBackupArgs{
		BackupFile: contextReader{ctx: ctx, Reader: tmp},
		Name:       newName,
	})
	return waitOp(ctx, op, err, "restore backup %q as %q", backup, newName)
}

// spoolBackup downloads a stored backup into a rewound temp file. The caller
// owns cleanup via cleanupExportTemp.
func (b *incusBackend) spoolBackup(ctx context.Context, instance, backup string) (*os.File, error) {
	tmp, err := os.CreateTemp("", "lexi-backup-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("spool backup %q of %q: %w", backup, instance, err)
	}
	canceler := cancel.NewHTTPRequestCanceller()
	stopCancel := context.AfterFunc(ctx, func() {
		if err := canceler.Cancel(); err != nil && canceler.Cancelable() {
			slog.Warn("cancel backup download", "instance", instance, "backup", backup, "err", err)
		}
	})
	defer stopCancel()

	if _, err := b.project(ctx).GetInstanceBackupFile(instance, backup, &incusclient.BackupFileRequest{
		BackupFile: contextWriteSeeker{ctx: ctx, WriteSeeker: tmp},
		Canceler:   canceler,
	}); err != nil {
		cleanupExportTemp(tmp)
		return nil, fmt.Errorf("download backup %q of %q: %w", backup, instance, mapErr(err))
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		cleanupExportTemp(tmp)
		return nil, fmt.Errorf("rewind backup %q of %q: %w", backup, instance, err)
	}
	return tmp, nil
}
