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

// fakeFile is one node in a fake instance's filesystem: either a directory or
// a regular file with content. Keys in the instance map are clean absolute
// paths and every directory is an explicit entry.
type fakeFile struct {
	dir     bool
	content []byte
	mode    string // e.g. "0644"
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

func (f *Fake) ListFiles(_ context.Context, instance, p string) ([]backend.FileEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[instance]
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

func (f *Fake) PullFile(_ context.Context, instance, p string, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[instance]
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

func (f *Fake) PushFile(_ context.Context, instance, p string, r io.Reader) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[instance]
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
	in.files[p] = &fakeFile{content: content, mode: "0644"}
	return nil
}

func (f *Fake) DeleteFile(_ context.Context, instance, p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[instance]
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

func (f *Fake) MakeDirectory(_ context.Context, instance, p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[instance]
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

// normalizePath requires an absolute path and strips a trailing slash (except
// for the root itself).
func normalizePath(p string) (string, error) {
	if !strings.HasPrefix(p, "/") {
		return "", invalid("path %q must be absolute", p)
	}
	if p != "/" {
		p = strings.TrimSuffix(p, "/")
	}
	return p, nil
}
