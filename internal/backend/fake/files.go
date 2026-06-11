package fake

import (
	"bytes"
	"context"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/adam/lxcon/internal/backend"
)

// fakeFile is one node in a fake instance's filesystem: a directory, symlink,
// or regular file with content. Keys in the instance map are clean absolute
// paths and every directory is an explicit entry.
type fakeFile struct {
	dir      bool
	symlink  bool
	content  []byte
	mode     string // e.g. "0644"
	uid, gid int64
}

// fileType is the node's FileInfo type string.
func (n *fakeFile) fileType() string {
	switch {
	case n.dir:
		return "directory"
	case n.symlink:
		return "symlink"
	default:
		return "file"
	}
}

// seedFiles is the starter filesystem every fake instance gets, so the Files
// tab has something to browse. Parent directories are seeded explicitly.
func seedFiles(name string) map[string]*fakeFile {
	return map[string]*fakeFile{
		"/":               {dir: true, mode: "0755"},
		"/etc":            {dir: true, mode: "0755"},
		"/root":           {dir: true, mode: "0755"},
		"/etc/hostname":   {content: []byte(name + "\n"), mode: "0644"},
		"/etc/os-release": {content: []byte("NAME=\"Fake Linux\"\n"), mode: "0644"},
		"/root/.profile":  {content: []byte("# ~/.profile\n"), mode: "0644"},
	}
}

// cloneFiles deep-copies a filesystem map so clones don't share nodes.
func cloneFiles(src map[string]*fakeFile) map[string]*fakeFile {
	out := make(map[string]*fakeFile, len(src))
	for k, v := range src {
		n := *v
		n.content = append([]byte(nil), v.content...)
		out[k] = &n
	}
	return out
}

func (f *Fake) ListFiles(ctx context.Context, instance, p string) ([]backend.FileEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[instance]
	if !ok {
		return nil, notFound(instance)
	}
	p, err := normalizePath(p)
	if err != nil {
		return nil, err
	}
	node, ok := in.files[p]
	if !ok {
		return nil, notFoundf("directory %q", p)
	}
	if !node.dir {
		return nil, invalid("%q is a file, not a directory", p)
	}

	prefix := p + "/"
	if p == "/" {
		prefix = "/"
	}
	var entries []backend.FileEntry
	for key, n := range in.files {
		rest, ok := strings.CutPrefix(key, prefix)
		if !ok || rest == "" || strings.Contains(rest, "/") {
			continue
		}
		entries = append(entries, backend.FileEntry{Name: rest, Dir: n.dir, Mode: n.mode})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

func (f *Fake) PullFile(ctx context.Context, instance, p string, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[instance]
	if !ok {
		return notFound(instance)
	}
	p, err := normalizePath(p)
	if err != nil {
		return err
	}
	node, ok := in.files[p]
	if !ok {
		return notFoundf("file %q", p)
	}
	if node.dir {
		return invalid("%q is a directory", p)
	}
	_, err = io.Copy(w, bytes.NewReader(node.content))
	return err
}

func (f *Fake) PushFile(ctx context.Context, instance, p string, r io.Reader, opts backend.FileWriteOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[instance]
	if !ok {
		return notFound(instance)
	}
	p, err := normalizePath(p)
	if err != nil {
		return err
	}
	if existing, ok := in.files[p]; ok && existing.dir {
		return invalid("%q is a directory", p)
	}
	// sftp semantics: push never creates parents.
	parent, ok := in.files[path.Dir(p)]
	if !ok {
		return notFoundf("directory %q", path.Dir(p))
	}
	if !parent.dir {
		return invalid("%q is a file, not a directory", path.Dir(p))
	}
	content, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	// Incus parity: the daemon ignores ownership/mode headers when
	// overwriting an existing file; options only apply on create.
	if existing, ok := in.files[p]; ok {
		existing.content = content
		return nil
	}
	mode := opts.Mode
	if mode == "" {
		mode = "0644"
	}
	in.files[p] = &fakeFile{content: content, mode: mode, uid: opts.UID, gid: opts.GID}
	return nil
}

func (f *Fake) PullFileInfo(ctx context.Context, instance, p string, w io.Writer, limit int64) (backend.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[instance]
	if !ok {
		return backend.FileInfo{}, notFound(instance)
	}
	p, err := normalizePath(p)
	if err != nil {
		return backend.FileInfo{}, err
	}
	node, ok := in.files[p]
	if !ok {
		return backend.FileInfo{}, notFoundf("file %q", p)
	}
	info := backend.FileInfo{Type: node.fileType(), Mode: node.mode, UID: node.uid, GID: node.gid}
	if node.dir || node.symlink {
		return info, nil
	}
	if limit > 0 && int64(len(node.content)) > limit {
		return backend.FileInfo{}, invalid("file %q exceeds the %d byte limit", p, limit)
	}
	if _, err := io.Copy(w, bytes.NewReader(node.content)); err != nil {
		return backend.FileInfo{}, err
	}
	return info, nil
}

func (f *Fake) PullFileHead(ctx context.Context, instance, p string, w io.Writer, limit int64) (backend.FileInfo, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[instance]
	if !ok {
		return backend.FileInfo{}, false, notFound(instance)
	}
	p, err := normalizePath(p)
	if err != nil {
		return backend.FileInfo{}, false, err
	}
	node, ok := in.files[p]
	if !ok {
		return backend.FileInfo{}, false, notFoundf("file %q", p)
	}
	info := backend.FileInfo{Type: node.fileType(), Mode: node.mode, UID: node.uid, GID: node.gid}
	if node.dir || node.symlink {
		return info, false, nil
	}
	content := node.content
	truncated := int64(len(content)) > limit
	if truncated {
		content = content[:limit]
	}
	if _, err := io.Copy(w, bytes.NewReader(content)); err != nil {
		return backend.FileInfo{}, false, err
	}
	return info, truncated, nil
}

// SeedSymlink plants a symlink node, which the file-transfer API cannot
// create; tests use it to exercise non-regular-file handling.
func (f *Fake) SeedSymlink(instance, p string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.spaceFor("default")

	if in, ok := sp.instances[instance]; ok {
		in.files[p] = &fakeFile{symlink: true, mode: "0777"}
	}
}

func (f *Fake) DeleteFile(ctx context.Context, instance, p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[instance]
	if !ok {
		return notFound(instance)
	}
	p, err := normalizePath(p)
	if err != nil {
		return err
	}
	if p == "/" {
		return invalid("cannot delete %q", p)
	}
	node, ok := in.files[p]
	if !ok {
		return notFoundf("file %q", p)
	}
	if node.dir {
		prefix := p + "/"
		for key := range in.files {
			if strings.HasPrefix(key, prefix) {
				return invalid("directory %q is not empty", p)
			}
		}
	}
	delete(in.files, p)
	return nil
}

func (f *Fake) MakeDirectory(ctx context.Context, instance, p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := f.space(ctx)

	in, ok := sp.instances[instance]
	if !ok {
		return notFound(instance)
	}
	p, err := normalizePath(p)
	if err != nil {
		return err
	}
	if _, ok := in.files[p]; ok {
		return conflict("%q already exists", p)
	}
	parent, ok := in.files[path.Dir(p)]
	if !ok {
		return notFoundf("directory %q", path.Dir(p))
	}
	if !parent.dir {
		return invalid("%q is a file, not a directory", path.Dir(p))
	}
	in.files[p] = &fakeFile{dir: true, mode: "0755"}
	return nil
}

// normalizePath requires an absolute path and canonicalizes it (dot segments,
// doubled and trailing slashes) so node keys stay in one form.
func normalizePath(p string) (string, error) {
	if !strings.HasPrefix(p, "/") {
		return "", invalid("path %q must be absolute", p)
	}
	return path.Clean(p), nil
}
