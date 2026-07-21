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

// ListVolumeBackups lists the named backups stored on the server for a custom
// volume, oldest first (the daemon already returns them in creation order).
func (b *incusBackend) ListVolumeBackups(ctx context.Context, pool, volume string) ([]backend.VolumeBackup, error) {
	bks, err := b.project(ctx).GetStorageVolumeBackups(pool, volume)
	if err != nil {
		return nil, fmt.Errorf("list backups of %q/%q: %w", pool, volume, mapErr(err))
	}
	out := make([]backend.VolumeBackup, 0, len(bks))
	for _, bk := range bks {
		out = append(out, backend.VolumeBackup{
			Name:       bk.Name,
			CreatedAt:  bk.CreatedAt,
			ExpiresAt:  bk.ExpiresAt,
			VolumeOnly: bk.VolumeOnly,
		})
	}
	return out, nil
}

// CreateVolumeBackup stores a named server-side backup. The daemon has no name
// default, so an empty name gets the CLI's backupN convention (first free
// index). A non-zero expiry needs the backup_expiry extension.
func (b *incusBackend) CreateVolumeBackup(ctx context.Context, pool, volume, name string, expiresAt time.Time, volumeOnly bool) error {
	srv := b.project(ctx)
	if !expiresAt.IsZero() && !b.server(ctx).HasExtension("backup_expiry") {
		return fmt.Errorf("backup expiry needs the daemon's backup_expiry extension: %w", backend.ErrUnsupported)
	}
	if name == "" {
		existing, err := srv.GetStorageVolumeBackups(pool, volume)
		if err != nil {
			return fmt.Errorf("list backups of %q/%q: %w", pool, volume, mapErr(err))
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
	op, err := srv.CreateStorageVolumeBackup(pool, volume, api.StorageVolumeBackupsPost{
		Name:                 name,
		ExpiresAt:            expiresAt,
		VolumeOnly:           volumeOnly,
		CompressionAlgorithm: "gzip",
	})
	return waitOp(ctx, op, err, "create backup %q of %q/%q", name, pool, volume)
}

func (b *incusBackend) DeleteVolumeBackup(ctx context.Context, pool, volume, backup string) error {
	op, err := b.project(ctx).DeleteStorageVolumeBackup(pool, volume, backup)
	return waitOp(ctx, op, err, "delete backup %q of %q/%q", backup, pool, volume)
}

// ExportVolumeBackup spools the stored backup to a temp file (the client needs
// an io.WriteSeeker) and streams it to w. The backup is persistent — nothing
// is created or cleaned up server-side.
func (b *incusBackend) ExportVolumeBackup(ctx context.Context, pool, volume, backup string, w io.Writer) error {
	tmp, err := b.spoolVolumeBackup(ctx, pool, volume, backup)
	if err != nil {
		return err
	}
	defer cleanupExportTemp(tmp)
	if _, err := io.Copy(w, tmp); err != nil {
		return fmt.Errorf("stream backup %q of %q/%q: %w", backup, pool, volume, err)
	}
	return nil
}

// RestoreVolumeBackup creates a new custom volume in targetPool from a stored
// backup. The tarball round-trips through a temp spool because the download
// needs a WriteSeeker; nothing crosses the browser.
func (b *incusBackend) RestoreVolumeBackup(ctx context.Context, pool, volume, backup, targetPool, newName string) error {
	tmp, err := b.spoolVolumeBackup(ctx, pool, volume, backup)
	if err != nil {
		return err
	}
	defer cleanupExportTemp(tmp)

	op, err := b.project(ctx).CreateStoragePoolVolumeFromBackup(targetPool, incusclient.StorageVolumeBackupArgs{
		BackupFile: contextReader{ctx: ctx, Reader: tmp},
		Name:       newName,
	})
	return waitOp(ctx, op, err, "restore backup %q as %q/%q", backup, targetPool, newName)
}

// spoolVolumeBackup downloads a stored backup into a rewound temp file. The
// caller owns cleanup via cleanupExportTemp.
func (b *incusBackend) spoolVolumeBackup(ctx context.Context, pool, volume, backup string) (*os.File, error) {
	tmp, err := os.CreateTemp("", "lexi-volbackup-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("spool backup %q of %q/%q: %w", backup, pool, volume, err)
	}
	canceler := cancel.NewHTTPRequestCanceller()
	stopCancel := context.AfterFunc(ctx, func() {
		if err := canceler.Cancel(); err != nil && canceler.Cancelable() {
			slog.Warn("cancel volume backup download", "pool", pool, "volume", volume, "backup", backup, "err", err)
		}
	})
	defer stopCancel()

	if _, err := b.project(ctx).GetStorageVolumeBackupFile(pool, volume, backup, &incusclient.BackupFileRequest{
		BackupFile: contextWriteSeeker{ctx: ctx, WriteSeeker: tmp},
		Canceler:   canceler,
	}); err != nil {
		cleanupExportTemp(tmp)
		return nil, fmt.Errorf("download backup %q of %q/%q: %w", backup, pool, volume, mapErr(err))
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		cleanupExportTemp(tmp)
		return nil, fmt.Errorf("rewind backup %q of %q/%q: %w", backup, pool, volume, err)
	}
	return tmp, nil
}
