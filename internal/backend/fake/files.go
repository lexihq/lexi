package fake

import (
	"bytes"
	"context"
	"io"
	"sort"
	"strings"

	"github.com/adam/lxcon/internal/backend"
)

// seedFiles is the starter filesystem every fake instance gets, so the Files
// tab has something to browse. Directories are implied by key prefixes.
func seedFiles(name string) map[string][]byte {
	return map[string][]byte{
		"/etc/hostname":   []byte(name + "\n"),
		"/etc/os-release": []byte("NAME=\"Fake Linux\"\n"),
		"/root/.profile":  []byte("# ~/.profile\n"),
	}
}

func (f *Fake) ListFiles(_ context.Context, instance, path string) ([]backend.FileEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[instance]
	if !ok {
		return nil, notFound(instance)
	}
	path, err := normalizePath(path)
	if err != nil {
		return nil, err
	}
	if _, ok := in.files[path]; ok {
		return nil, invalid("%q is a file, not a directory", path)
	}

	prefix := path + "/"
	if path == "/" {
		prefix = "/"
	}
	seen := map[string]bool{}
	var entries []backend.FileEntry
	for key := range in.files {
		rest, ok := strings.CutPrefix(key, prefix)
		if !ok || rest == "" {
			continue
		}
		name, deeper := rest, false
		if before, _, found := strings.Cut(rest, "/"); found {
			name, deeper = before, true
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		if deeper {
			entries = append(entries, backend.FileEntry{Name: name, Dir: true, Mode: "0755"})
		} else {
			entries = append(entries, backend.FileEntry{Name: name, Mode: "0644"})
		}
	}
	if len(entries) == 0 && path != "/" {
		return nil, notFoundf("directory %q", path)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

func (f *Fake) PullFile(_ context.Context, instance, path string, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[instance]
	if !ok {
		return notFound(instance)
	}
	path, err := normalizePath(path)
	if err != nil {
		return err
	}
	content, ok := in.files[path]
	if !ok {
		if f.impliedDir(in, path) {
			return invalid("%q is a directory", path)
		}
		return notFoundf("file %q", path)
	}
	_, err = io.Copy(w, bytes.NewReader(content))
	return err
}

func (f *Fake) PushFile(_ context.Context, instance, path string, r io.Reader) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	in, ok := f.instances[instance]
	if !ok {
		return notFound(instance)
	}
	path, err := normalizePath(path)
	if err != nil {
		return err
	}
	if path == "/" || f.impliedDir(in, path) {
		return invalid("%q is a directory", path)
	}
	content, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if in.files == nil {
		in.files = map[string][]byte{}
	}
	in.files[path] = content
	return nil
}

// impliedDir reports whether path exists as a directory implied by deeper
// files. Callers must hold the mutex.
func (f *Fake) impliedDir(in *instance, path string) bool {
	prefix := path + "/"
	if path == "/" {
		return true
	}
	for key := range in.files {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// normalizePath requires an absolute path and strips a trailing slash (except
// for the root itself).
func normalizePath(path string) (string, error) {
	if !strings.HasPrefix(path, "/") {
		return "", invalid("path %q must be absolute", path)
	}
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}
	return path, nil
}
