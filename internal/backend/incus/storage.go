package incus

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/lexihq/lexi/internal/backend"
	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/lxc/incus/v6/shared/cancel"
)

func (b *incusBackend) ListStoragePools(ctx context.Context) ([]backend.StoragePool, error) {
	client := b.project(ctx)
	ps, err := client.GetStoragePools()
	if err != nil {
		return nil, fmt.Errorf("list storage pools: %w", mapErr(err))
	}
	out := make([]backend.StoragePool, 0, len(ps))
	for i := range ps {
		out = append(out, toPool(&ps[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	// Capacity is best-effort: the list must render even when a pool's
	// resources endpoint fails (e.g. an unavailable remote ceph pool). Fetched
	// concurrently — this list also feeds the instance page's create dialog,
	// so N serial round trips would gate the most-visited page on the slowest
	// pool.
	var wg sync.WaitGroup
	for i := range out {
		wg.Go(func() {
			if res, err := client.GetStoragePoolResources(out[i].Name); err == nil {
				out[i].SpaceUsed = clampToInt64(res.Space.Used)
				out[i].SpaceTotal = clampToInt64(res.Space.Total)
			} else {
				slog.Warn("storage pool resources", "pool", out[i].Name, "err", err)
			}
		})
	}
	wg.Wait()
	return out, nil
}

// clampToInt64 converts a daemon uint64 byte count into the domain's int64,
// clamping instead of overflowing on absurd values.
func clampToInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

func (b *incusBackend) GetStoragePool(ctx context.Context, pool string) (backend.StoragePool, error) {
	client := b.project(ctx)
	p, etag, err := client.GetStoragePool(pool)
	if err != nil {
		return backend.StoragePool{}, fmt.Errorf("get storage pool %q: %w", pool, mapErr(err))
	}
	out := toPool(p)
	out.Version = backend.Version(etag)
	// Same best-effort capacity as the list (the detail header shows it too).
	if res, err := client.GetStoragePoolResources(pool); err == nil {
		out.SpaceUsed = clampToInt64(res.Space.Used)
		out.SpaceTotal = clampToInt64(res.Space.Total)
	} else {
		slog.Warn("storage pool resources", "pool", pool, "err", err)
	}
	return out, nil
}

// UpdateStoragePool updates the pool's description and replaces its config via
// GET-preserve-PUT. The version is the etag from GetStoragePool; the daemon
// rejects the PUT with 412 (mapped to ErrConflict) when the pool changed since
// that read. An empty version updates unconditionally. Immutable config keys
// (driver-specific, e.g. zfs.pool_name) are rejected by the daemon with a 400.
func (b *incusBackend) UpdateStoragePool(ctx context.Context, name, description string, config map[string]string, version backend.Version) error {
	p, _, err := b.project(ctx).GetStoragePool(name)
	if err != nil {
		return fmt.Errorf("get storage pool %q: %w", name, mapErr(err))
	}
	put := p.Writable()
	put.Description = description
	put.Config = config
	if err := b.project(ctx).UpdateStoragePool(name, put, string(version)); err != nil {
		return fmt.Errorf("update storage pool %q: %w", name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) CreateStoragePool(ctx context.Context, p backend.StoragePool) error {
	post := api.StoragePoolsPost{Name: p.Name, Driver: p.Driver}
	post.Description = p.Description
	post.Config = p.Config
	if err := b.project(ctx).CreateStoragePool(post); err != nil {
		return fmt.Errorf("create storage pool %q: %w", p.Name, mapErr(err))
	}
	return nil
}

// DeleteStoragePool pre-checks UsedBy (profiles count too) so a referenced
// pool conflicts cleanly; a reference appearing in the stat-then-delete window
// surfaces as the daemon's own 400, which is acceptable.
func (b *incusBackend) DeleteStoragePool(ctx context.Context, name string) error {
	p, _, err := b.project(ctx).GetStoragePool(name)
	if err != nil {
		return fmt.Errorf("delete storage pool %q: %w", name, mapErr(err))
	}
	if len(p.UsedBy) > 0 {
		return fmt.Errorf("delete storage pool %q: in use by %s: %w", name, strings.Join(p.UsedBy, ", "), backend.ErrConflict)
	}
	if err := b.project(ctx).DeleteStoragePool(name); err != nil {
		return fmt.Errorf("delete storage pool %q: %w", name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) ListVolumes(ctx context.Context, pool string) ([]backend.StorageVolume, error) {
	vs, err := b.project(ctx).GetStoragePoolVolumes(pool)
	if err != nil {
		return nil, fmt.Errorf("list volumes in %q: %w", pool, mapErr(err))
	}
	out := make([]backend.StorageVolume, 0)
	for i := range vs {
		if vs[i].Type == "custom" {
			out = append(out, toVolume(pool, &vs[i]))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (b *incusBackend) GetVolume(ctx context.Context, pool, name string) (backend.StorageVolume, error) {
	v, etag, err := b.project(ctx).GetStoragePoolVolume(pool, "custom", name)
	if err != nil {
		return backend.StorageVolume{}, fmt.Errorf("get volume %q/%q: %w", pool, name, mapErr(err))
	}
	out := toVolume(pool, v)
	out.Version = backend.Version(etag)
	return out, nil
}

// UpdateVolume updates the volume's description and replaces its config via
// GET-preserve-PUT (UpdateStoragePoolVolume is synchronous). The version is the
// etag from GetVolume; 412 → ErrConflict. An empty version updates
// unconditionally. The daemon computes the volume etag from name/type/config
// only — description is excluded — so concurrent description-only edits cannot
// conflict and are last-write-wins.
func (b *incusBackend) UpdateVolume(ctx context.Context, pool, name, description string, config map[string]string, version backend.Version) error {
	v, _, err := b.project(ctx).GetStoragePoolVolume(pool, "custom", name)
	if err != nil {
		return fmt.Errorf("get volume %q/%q: %w", pool, name, mapErr(err))
	}
	put := v.Writable()
	put.Description = description
	put.Config = config
	if err := b.project(ctx).UpdateStoragePoolVolume(pool, "custom", name, put, string(version)); err != nil {
		return fmt.Errorf("update volume %q/%q: %w", pool, name, mapErr(err))
	}
	return nil
}

// RenameVolume renames a custom volume (synchronous). The target name is
// pre-checked so a collision is a deterministic ErrConflict regardless of the
// daemon's backend-specific error wording; a volume attached to an instance is
// refused by the daemon itself.
func (b *incusBackend) RenameVolume(ctx context.Context, pool, name, newName string) error {
	vols, err := b.ListVolumes(ctx, pool)
	if err != nil {
		return err
	}
	var sourceExists, targetTaken bool
	for _, v := range vols {
		sourceExists = sourceExists || v.Name == name
		targetTaken = targetTaken || v.Name == newName
	}
	// Source first, so a missing volume is not-found even when the target name
	// is taken (matching the fake's lookup order).
	if !sourceExists {
		return fmt.Errorf("volume %q/%q: %w", pool, name, backend.ErrNotFound)
	}
	if targetTaken {
		return fmt.Errorf("volume %q already exists: %w", newName, backend.ErrConflict)
	}
	if err := b.project(ctx).RenameStoragePoolVolume(pool, "custom", name, api.StorageVolumePost{Name: newName}); err != nil {
		return fmt.Errorf("rename volume %q/%q: %w", pool, name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) CreateVolume(ctx context.Context, pool string, v backend.StorageVolume) error {
	post := api.StorageVolumesPost{Name: v.Name, Type: "custom", ContentType: v.ContentType}
	post.Config = v.Config
	if err := b.project(ctx).CreateStoragePoolVolume(pool, post); err != nil {
		return fmt.Errorf("create volume %q/%q: %w", pool, v.Name, mapErr(err))
	}
	return nil
}

func (b *incusBackend) DeleteVolume(ctx context.Context, pool, name string) error {
	if err := b.project(ctx).DeleteStoragePoolVolume(pool, "custom", name); err != nil {
		return fmt.Errorf("delete volume %q/%q: %w", pool, name, mapErr(err))
	}
	return nil
}

func toPool(p *api.StoragePool) backend.StoragePool {
	return backend.StoragePool{Name: p.Name, Driver: p.Driver, Description: p.Description, Config: p.Config, UsedBy: p.UsedBy}
}

func toVolume(pool string, v *api.StorageVolume) backend.StorageVolume {
	return backend.StorageVolume{Name: v.Name, Type: v.Type, ContentType: v.ContentType, Pool: pool, Description: v.Description, Config: v.Config, UsedBy: v.UsedBy}
}

func (b *incusBackend) ListVolumeSnapshots(ctx context.Context, pool, volume string) ([]backend.StorageVolumeSnapshot, error) {
	ss, err := b.project(ctx).GetStoragePoolVolumeSnapshots(pool, "custom", volume)
	if err != nil {
		return nil, fmt.Errorf("list snapshots %q/%q: %w", pool, volume, mapErr(err))
	}
	out := make([]backend.StorageVolumeSnapshot, 0, len(ss))
	for i := range ss {
		out = append(out, toVolumeSnapshot(&ss[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (b *incusBackend) CreateVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error {
	op, err := b.project(ctx).CreateStoragePoolVolumeSnapshot(pool, "custom", volume, api.StorageVolumeSnapshotsPost{Name: snapshot})
	return waitOp(ctx, op, err, "snapshot volume %q/%q", pool, volume)
}

func (b *incusBackend) DeleteVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error {
	op, err := b.project(ctx).DeleteStoragePoolVolumeSnapshot(pool, "custom", volume, snapshot)
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
	op, err := b.project(ctx).RenameStoragePoolVolumeSnapshot(pool, "custom", volume, snapshot, api.StorageVolumeSnapshotPost{Name: newName})
	return waitOp(ctx, op, err, "rename snapshot %q/%q/%q", pool, volume, snapshot)
}

// UpdateVolumeSnapshotExpiry does a GET-preserve-PUT setting ExpiresAt; a zero
// time clears it (nil pointer). UpdateStoragePoolVolumeSnapshot is synchronous.
func (b *incusBackend) UpdateVolumeSnapshotExpiry(ctx context.Context, pool, volume, snapshot string, expiresAt time.Time) error {
	s, etag, err := b.project(ctx).GetStoragePoolVolumeSnapshot(pool, "custom", volume, snapshot)
	if err != nil {
		return fmt.Errorf("get snapshot %q/%q/%q: %w", pool, volume, snapshot, mapErr(err))
	}
	put := s.Writable()
	if expiresAt.IsZero() {
		put.ExpiresAt = nil
	} else {
		put.ExpiresAt = &expiresAt
	}
	if err := b.project(ctx).UpdateStoragePoolVolumeSnapshot(pool, "custom", volume, snapshot, put, etag); err != nil {
		return fmt.Errorf("update snapshot expiry %q/%q/%q: %w", pool, volume, snapshot, mapErr(err))
	}
	return nil
}

// RestoreVolumeSnapshot does a GET-then-PUT setting put.Restore.
// UpdateStoragePoolVolume is synchronous (no operation to wait on).
func (b *incusBackend) RestoreVolumeSnapshot(ctx context.Context, pool, volume, snapshot string) error {
	v, etag, err := b.project(ctx).GetStoragePoolVolume(pool, "custom", volume)
	if err != nil {
		return fmt.Errorf("get volume %q/%q: %w", pool, volume, mapErr(err))
	}
	put := v.Writable()
	put.Restore = snapshot
	if err := b.project(ctx).UpdateStoragePoolVolume(pool, "custom", volume, put, etag); err != nil {
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

// ExportVolume creates a server-side volume backup, spools it to a temp file
// (the client API needs an io.WriteSeeker), then streams it to w — the same
// flow as ExportInstance, one level down. The temporary backup is removed via
// deferred best-effort cleanup on every path.
func (b *incusBackend) ExportVolume(ctx context.Context, pool, volume string, w io.Writer) error {
	backupName := fmt.Sprintf("lexi-export-%d", time.Now().UnixNano())

	// Capture the scoped client once: the deferred cleanup runs under its own
	// detached context and must still target the request's project.
	srv := b.project(ctx)
	op, err := srv.CreateStorageVolumeBackup(pool, volume, api.StorageVolumeBackupsPost{
		Name:                 backupName,
		CompressionAlgorithm: "gzip",
	})
	if err != nil {
		return fmt.Errorf("create backup of volume %q/%q: %w", pool, volume, mapErr(err))
	}
	// Once the operation exists, clean up on every return path; a missing
	// backup is a no-op inside deleteVolumeBackup.
	defer b.deleteVolumeBackup(srv, pool, volume, backupName)
	if err := op.WaitContext(ctx); err != nil {
		// A canceled wait leaves the server operation running; cancel it so
		// the backup does not finish and leak after we have given up.
		if ctx.Err() != nil {
			if cancelErr := op.Cancel(); cancelErr != nil {
				slog.Warn("cancel volume backup operation", "pool", pool, "volume", volume, "err", cancelErr)
			}
		}
		return fmt.Errorf("create backup of volume %q/%q: %w", pool, volume, mapErr(err))
	}

	tmp, err := os.CreateTemp("", "lexi-volume-export-*.tar.gz")
	if err != nil {
		return fmt.Errorf("spool backup of volume %q/%q: %w", pool, volume, err)
	}
	defer cleanupExportTemp(tmp)

	if err := ctx.Err(); err != nil {
		return err
	}
	canceler := cancel.NewHTTPRequestCanceller()
	stopCancel := context.AfterFunc(ctx, func() {
		if err := canceler.Cancel(); err != nil && canceler.Cancelable() {
			slog.Warn("cancel volume backup download", "pool", pool, "volume", volume, "err", err)
		}
	})
	defer stopCancel()

	if _, err := srv.GetStorageVolumeBackupFile(pool, volume, backupName, &incusclient.BackupFileRequest{
		BackupFile: contextWriteSeeker{ctx: ctx, WriteSeeker: tmp},
		Canceler:   canceler,
	}); err != nil {
		return fmt.Errorf("download backup of volume %q/%q: %w", pool, volume, mapErr(err))
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind backup of volume %q/%q: %w", pool, volume, err)
	}
	if _, err := io.Copy(w, tmp); err != nil {
		return fmt.Errorf("stream backup of volume %q/%q: %w", pool, volume, err)
	}
	return nil
}

// ImportVolume creates custom volume volume in pool from a backup tarball
// streamed from r (as produced by ExportVolume). Naming the new volume needs
// the daemon's backup_override_name extension (gated by caps.VolumeBackups).
func (b *incusBackend) ImportVolume(ctx context.Context, pool, volume string, r io.Reader) error {
	// The daemon skips name validation on the backup-import override path
	// (normal volume creation runs validate.IsAPIName), so an invalid name
	// would land in its database — pre-check here, mirroring the fake.
	if !validAPIName(volume) {
		return fmt.Errorf("invalid volume name %q: %w", volume, backend.ErrInvalid)
	}
	rest, err := rejectNonCustomBackup(r)
	if err != nil {
		return fmt.Errorf("import volume %q/%q: %w", pool, volume, err)
	}
	op, err := b.project(ctx).CreateStoragePoolVolumeFromBackup(pool, incusclient.StorageVolumeBackupArgs{
		BackupFile: contextReader{ctx: ctx, Reader: rest},
		Name:       volume,
	})
	if err != nil {
		return fmt.Errorf("import volume %q/%q: %w", pool, volume, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		if ctx.Err() != nil {
			if cancelErr := op.Cancel(); cancelErr != nil {
				slog.Warn("cancel volume import operation", "pool", pool, "volume", volume, "err", cancelErr)
			}
		}
		// A concurrent same-name import race loses on the database unique
		// constraint, whose message lacks "already exists"; mapErr turns it
		// into ErrConflict.
		return fmt.Errorf("import volume %q/%q: %w", pool, volume, mapErr(err))
	}
	return nil
}

// CreateVolumeFromISO creates a custom "iso" content-type volume in pool by
// streaming the ISO image from r (gated by caps.ISOVolumes over the
// custom_volume_iso extension). Like the backup-import override path, the
// daemon names the volume from a header without running its usual name
// validation — pre-check here, mirroring the fake.
func (b *incusBackend) CreateVolumeFromISO(ctx context.Context, pool, volume string, r io.Reader) error {
	if !validAPIName(volume) {
		return fmt.Errorf("invalid volume name %q: %w", volume, backend.ErrInvalid)
	}
	op, err := b.project(ctx).CreateStoragePoolVolumeFromISO(pool, incusclient.StorageVolumeBackupArgs{
		BackupFile: contextReader{ctx: ctx, Reader: r},
		Name:       volume,
	})
	if err != nil {
		return fmt.Errorf("create ISO volume %q/%q: %w", pool, volume, mapErr(err))
	}
	if err := op.WaitContext(ctx); err != nil {
		if ctx.Err() != nil {
			if cancelErr := op.Cancel(); cancelErr != nil {
				slog.Warn("cancel ISO volume upload operation", "pool", pool, "volume", volume, "err", cancelErr)
			}
		}
		// Like ImportVolume, a concurrent same-name upload loses on the
		// database unique constraint; mapErr turns it into ErrConflict.
		return fmt.Errorf("create ISO volume %q/%q: %w", pool, volume, mapErr(err))
	}
	return nil
}

// validBaseName mirrors the daemon's validate.IsAPIName character rules (≤64
// chars, no whitespace, none of the reserved URL characters) WITHOUT the
// start/end-alphanumeric (min-two) rule apiNameEnds layers on top. Instance
// names follow the looser hostname rules, so a single-character instance name
// is legal; RenameInstance validates against this, matching the fake.
func validBaseName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return !strings.ContainsAny(name, `$?&+"'`+"`*/")
}

// validAPIName mirrors the daemon's full validate.IsAPIName rules (validBaseName
// plus the start/end-alphanumeric rule, which implies a minimum of two
// characters), matching the fake's validator. It applies where the daemon runs
// the full IsAPIName (projects, volume creation), not to instance names.
func validAPIName(name string) bool {
	return validBaseName(name) && apiNameEnds.MatchString(name)
}

var apiNameEnds = regexp.MustCompile(`^[a-zA-Z0-9]+.*[a-zA-Z0-9]+$`)

// rejectNonCustomBackup peeks backup/index.yaml from the upload and refuses
// instance backups (type container/virtual-machine) with ErrInvalid — the
// daemon never checks the type on this endpoint and fails such uploads deep
// in the volume unpacker with an opaque internal error. Detection is
// best-effort over the first 64KiB of gzip or plain tar (our exports are
// gzip); anything unrecognized passes through for the daemon to judge. The
// returned reader replays the peeked bytes followed by the rest of r.
func rejectNonCustomBackup(r io.Reader) (io.Reader, error) {
	head := make([]byte, 64<<10)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	head = head[:n]
	rest := io.MultiReader(bytes.NewReader(head), r)

	var tarStream io.Reader = bytes.NewReader(head)
	if bytes.HasPrefix(head, []byte{0x1f, 0x8b}) {
		gz, err := gzip.NewReader(bytes.NewReader(head))
		if err != nil {
			return rest, nil
		}
		tarStream = gz
	}
	// index.yaml is the first entry of every incus backup tarball; a
	// truncated or foreign stream simply skips the check.
	tr := tar.NewReader(tarStream)
	hdr, err := tr.Next()
	if err != nil || path.Clean(hdr.Name) != "backup/index.yaml" {
		return rest, nil
	}
	// ReadAll may error where the peek window ends; whatever was decoded up
	// to that point still carries the top-level type line.
	data, err := io.ReadAll(io.LimitReader(tr, 256<<10))
	if err != nil && len(data) == 0 {
		return rest, nil
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		// Top-level field only: nested config blocks indent their keys.
		t, ok := strings.CutPrefix(line, "type:")
		if !ok {
			continue
		}
		t = strings.TrimSpace(strings.TrimSuffix(t, "\r"))
		if t == "container" || t == "virtual-machine" {
			return nil, fmt.Errorf("backup is an instance backup (type %q), not a volume backup: %w", t, backend.ErrInvalid)
		}
		break
	}
	return rest, nil
}

// deleteVolumeBackup removes the temporary server-side backup created during
// export — deleteBackup's volume sibling: best-effort, detached context,
// log-only (a failure cannot change the already-streamed result).
func (b *incusBackend) deleteVolumeBackup(srv incusclient.InstanceServer, pool, volume, backupName string) {
	ctx, cancel := context.WithTimeout(context.Background(), backupDeleteTimeout)
	defer cancel()

	op, err := srv.DeleteStorageVolumeBackup(pool, volume, backupName)
	if err != nil {
		if !errors.Is(mapErr(err), backend.ErrNotFound) {
			slog.Warn("delete volume export backup", "backup", backupName, "pool", pool, "volume", volume, "err", err)
		}
		return
	}
	if err := op.WaitContext(ctx); err != nil {
		slog.Warn("await deletion of volume export backup", "backup", backupName, "pool", pool, "volume", volume, "err", err)
	}
}
