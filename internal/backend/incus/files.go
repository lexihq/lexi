package incus

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	incusclient "github.com/lxc/incus/v6/client"
)

// ListFiles lists the instance directory at path. The file API returns entry
// names only, so each entry is statted (opened and immediately closed) for its
// type and mode; a stat failure (e.g. a dangling symlink) yields the entry
// with an empty Mode rather than failing the whole listing.
func (b *incusBackend) ListFiles(_ context.Context, instance, path string) ([]backend.FileEntry, error) {
	content, resp, err := b.srv.GetInstanceFile(instance, path)
	if err != nil {
		return nil, fmt.Errorf("list files %q: %w", path, mapErr(err))
	}
	closeAndLogFile(path, content)
	if resp.Type != "directory" {
		return nil, fmt.Errorf("list files: %q is not a directory: %w", path, backend.ErrInvalid)
	}

	prefix := path + "/"
	if path == "/" {
		prefix = "/"
	}
	entries := make([]backend.FileEntry, 0, len(resp.Entries))
	for _, name := range resp.Entries {
		entry := backend.FileEntry{Name: name}
		if c, r, err := b.srv.GetInstanceFile(instance, prefix+name); err == nil {
			closeAndLogFile(prefix+name, c)
			entry.Dir = r.Type == "directory"
			entry.Mode = fmt.Sprintf("%04o", r.Mode)
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

// PullFile streams the instance file at path to w.
func (b *incusBackend) PullFile(_ context.Context, instance, path string, w io.Writer) error {
	content, resp, err := b.srv.GetInstanceFile(instance, path)
	if err != nil {
		return fmt.Errorf("pull file %q: %w", path, mapErr(err))
	}
	defer closeAndLogFile(path, content)
	if resp.Type == "directory" {
		return fmt.Errorf("pull file: %q is a directory: %w", path, backend.ErrInvalid)
	}
	if _, err := io.Copy(w, content); err != nil {
		return fmt.Errorf("pull file %q: %w", path, err)
	}
	return nil
}

// PushFile creates (or overwrites) the instance file at path from r with the
// given ownership and mode (zero opts: root:root 0644). The file API needs a
// ReadSeeker, so the content is buffered; the HTTP handler caps the upload size.
func (b *incusBackend) PushFile(_ context.Context, instance, path string, r io.Reader, opts backend.FileWriteOptions) error {
	mode := 0o644
	if opts.Mode != "" {
		m, err := strconv.ParseInt(opts.Mode, 8, 32)
		if err != nil {
			return fmt.Errorf("push file %q: bad mode %q: %w", path, opts.Mode, backend.ErrInvalid)
		}
		mode = int(m)
	}
	content, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("push file %q: %w", path, err)
	}
	err = b.srv.CreateInstanceFile(instance, path, incusclient.InstanceFileArgs{
		Content:   bytes.NewReader(content),
		Mode:      mode,
		UID:       opts.UID,
		GID:       opts.GID,
		Type:      "file",
		WriteMode: "overwrite",
	})
	if err != nil {
		return fmt.Errorf("push file %q: %w", path, mapErr(err))
	}
	return nil
}

// PullFileInfo streams the file at path to w and returns its metadata.
// Directories and symlinks report their type without content; a limit > 0
// rejects larger files with ErrInvalid instead of streaming them fully.
func (b *incusBackend) PullFileInfo(_ context.Context, instance, path string, w io.Writer, limit int64) (backend.FileInfo, error) {
	content, resp, err := b.srv.GetInstanceFile(instance, path)
	if err != nil {
		return backend.FileInfo{}, fmt.Errorf("pull file %q: %w", path, mapErr(err))
	}
	defer closeAndLogFile(path, content)
	info := backend.FileInfo{Type: resp.Type, Mode: fmt.Sprintf("%04o", resp.Mode), UID: resp.UID, GID: resp.GID}
	if resp.Type != "file" || content == nil {
		return info, nil
	}
	src := io.Reader(content)
	if limit > 0 {
		src = io.LimitReader(content, limit+1)
	}
	written, err := io.Copy(w, src)
	if err != nil {
		return backend.FileInfo{}, fmt.Errorf("pull file %q: %w", path, err)
	}
	if limit > 0 && written > limit {
		return backend.FileInfo{}, fmt.Errorf("file %q exceeds the %d byte limit: %w", path, limit, backend.ErrInvalid)
	}
	return info, nil
}

// DeleteFile removes the instance file at path. The daemon API is
// non-recursive: directories must be empty, and deleting "/" is rejected.
func (b *incusBackend) DeleteFile(_ context.Context, instance, path string) error {
	if path == "/" {
		return fmt.Errorf("delete file: cannot delete %q: %w", path, backend.ErrInvalid)
	}
	if err := b.srv.DeleteInstanceFile(instance, path); err != nil {
		// The daemon reports a non-empty directory as a generic sftp failure;
		// surface it as a user error, not a 500.
		if strings.Contains(err.Error(), "directory not empty") {
			return fmt.Errorf("delete file %q: %w: %w", path, backend.ErrInvalid, err)
		}
		return fmt.Errorf("delete file %q: %w", path, mapErr(err))
	}
	return nil
}

// MakeDirectory creates a root-owned 0755 directory at path (parents must
// exist). The daemon silently succeeds when anything — even a regular file —
// already exists at path, so existence is pre-checked to surface a conflict
// like the fake does; the stat-then-create race window is accepted.
func (b *incusBackend) MakeDirectory(_ context.Context, instance, path string) error {
	if content, _, err := b.srv.GetInstanceFile(instance, path); err == nil {
		closeAndLogFile(path, content)
		return fmt.Errorf("make directory: %q already exists: %w", path, backend.ErrConflict)
	}
	err := b.srv.CreateInstanceFile(instance, path, incusclient.InstanceFileArgs{
		Type: "directory",
		Mode: 0o755,
	})
	if err != nil {
		return fmt.Errorf("make directory %q: %w", path, mapErr(err))
	}
	return nil
}

// closeAndLogFile closes a file-content reader, logging (not failing) close
// errors — the content has either been fully consumed or deliberately skipped.
// The reader is nil for directories (the client returns no body for them).
func closeAndLogFile(path string, c io.Closer) {
	if c == nil {
		return
	}
	if err := c.Close(); err != nil {
		slog.Warn("close instance file", "path", path, "err", err)
	}
}
