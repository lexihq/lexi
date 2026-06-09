package incus

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"

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

// PushFile creates (or overwrites) the instance file at path from r, owned by
// root with mode 0644. The file API needs a ReadSeeker, so the content is
// buffered; the HTTP handler caps the upload size.
func (b *incusBackend) PushFile(_ context.Context, instance, path string, r io.Reader) error {
	content, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("push file %q: %w", path, err)
	}
	err = b.srv.CreateInstanceFile(instance, path, incusclient.InstanceFileArgs{
		Content:   bytes.NewReader(content),
		Mode:      0o644,
		Type:      "file",
		WriteMode: "overwrite",
	})
	if err != nil {
		return fmt.Errorf("push file %q: %w", path, mapErr(err))
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
