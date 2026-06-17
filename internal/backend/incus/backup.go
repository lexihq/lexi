package incus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/lexihq/lexi/internal/backend"
	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/lxc/incus/v6/shared/cancel"
)

// ExportInstance creates a backup, spools it to a temp file (the client API
// needs an io.WriteSeeker), then streams the spooled file to w. The server-side
// backup is removed via a deferred best-effort cleanup so it is deleted on every
// path, including errors between creation and streaming. The backup name is
// timestamped to avoid colliding with concurrent runs.
func (b *incusBackend) ExportInstance(ctx context.Context, name string, w io.Writer) error {
	backupName := fmt.Sprintf("lexi-export-%d", time.Now().UnixNano())

	// Capture the scoped client once: the deferred cleanup runs under its own
	// detached context and must still target the request's project.
	srv := b.project(ctx)
	op, err := srv.CreateInstanceBackup(name, api.InstanceBackupsPost{
		Name:                 backupName,
		CompressionAlgorithm: "gzip",
	})
	if err != nil {
		return fmt.Errorf("create backup of %q: %w", name, mapErr(err))
	}
	// Once the operation exists, clean up on every return path. deleteBackup
	// treats a missing backup as a no-op, so this is harmless if creation failed.
	defer b.deleteBackup(srv, name, backupName)
	if err := op.WaitContext(ctx); err != nil {
		// A canceled wait leaves the server operation running; cancel it so the
		// backup does not finish and leak after we have given up. The deferred
		// cleanup covers the race where it completes before the cancel lands.
		if ctx.Err() != nil {
			if cancelErr := op.Cancel(); cancelErr != nil {
				slog.Warn("cancel backup operation", "instance", name, "err", cancelErr)
			}
		}
		return fmt.Errorf("create backup of %q: %w", name, mapErr(err))
	}

	tmp, err := os.CreateTemp("", "lexi-export-*.tar.gz")
	if err != nil {
		return fmt.Errorf("spool backup of %q: %w", name, err)
	}
	defer cleanupExportTemp(tmp)

	if err := ctx.Err(); err != nil {
		return err
	}
	canceler := cancel.NewHTTPRequestCanceller()
	stopCancel := context.AfterFunc(ctx, func() {
		if err := canceler.Cancel(); err != nil && canceler.Cancelable() {
			slog.Warn("cancel backup download", "instance", name, "err", err)
		}
	})
	defer stopCancel()

	if _, err := srv.GetInstanceBackupFile(name, backupName, &incusclient.BackupFileRequest{
		BackupFile: contextWriteSeeker{ctx: ctx, WriteSeeker: tmp},
		Canceler:   canceler,
	}); err != nil {
		return fmt.Errorf("download backup of %q: %w", name, mapErr(err))
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind backup of %q: %w", name, err)
	}
	if _, err := io.Copy(w, tmp); err != nil {
		return fmt.Errorf("stream backup of %q: %w", name, err)
	}
	return nil
}

// ImportInstance creates an instance named name from a backup tarball streamed
// from r (as produced by ExportInstance).
func (b *incusBackend) ImportInstance(ctx context.Context, name string, r io.Reader) error {
	op, err := b.project(ctx).CreateInstanceFromBackup(incusclient.InstanceBackupArgs{
		BackupFile: contextReader{ctx: ctx, Reader: r},
		Name:       name,
	})
	if err != nil {
		return fmt.Errorf("import instance %q: %w", name, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		if ctx.Err() != nil {
			if cancelErr := op.Cancel(); cancelErr != nil {
				slog.Warn("cancel import operation", "instance", name, "err", cancelErr)
			}
		}
		return fmt.Errorf("import instance %q: %w", name, mapErr(err))
	}
	return nil
}

type contextReader struct {
	io.Reader

	ctx context.Context
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.Reader.Read(p)
	if ctxErr := r.ctx.Err(); ctxErr != nil {
		return n, ctxErr
	}
	return n, err
}

type contextWriteSeeker struct {
	io.WriteSeeker

	ctx context.Context
}

func (w contextWriteSeeker) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.WriteSeeker.Write(p)
}

func cleanupExportTemp(tmp *os.File) {
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		slog.Warn("close export temp file", "path", path, "err", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("remove export temp file", "path", path, "err", err)
	}
}

// deleteBackup removes the temporary server-side backup created during export.
// It is best-effort cleanup invoked via defer with its own bounded context,
// detached from the request: a client disconnecting as the download finishes
// must not abort cleanup and leak the backup. A failure cannot change the
// already-streamed result, so it is logged (not returned) to keep leaked backups
// discoverable; a missing backup means there was nothing to clean and is ignored.
func (b *incusBackend) deleteBackup(srv incusclient.InstanceServer, name, backupName string) {
	ctx, cancel := context.WithTimeout(context.Background(), backupDeleteTimeout)
	defer cancel()

	op, err := srv.DeleteInstanceBackup(name, backupName)
	if err != nil {
		if !errors.Is(mapErr(err), backend.ErrNotFound) {
			slog.Warn("delete export backup", "backup", backupName, "instance", name, "err", err)
		}
		return
	}
	if err := op.WaitContext(ctx); err != nil {
		slog.Warn("await deletion of export backup", "backup", backupName, "instance", name, "err", err)
	}
}
