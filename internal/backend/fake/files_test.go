package fake

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/adam/lxcon/internal/backend"
)

// mustList lists a directory, failing the test on error.
func mustList(t *testing.T, b *Fake, instance, path string) []backend.FileEntry {
	t.Helper()
	entries, err := b.ListFiles(ctx(), instance, path)
	if err != nil {
		t.Fatalf("list files %s: %v", path, err)
	}
	return entries
}

// entryNamed returns the entry with the given name, or nil.
func entryNamed(entries []backend.FileEntry, name string) *backend.FileEntry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}

func TestListFilesRootSeeded(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	entries := mustList(t, b, "demo", "/")

	etc := entryNamed(entries, "etc")
	if etc == nil || !etc.Dir {
		t.Fatalf("expected a seeded etc directory, got %+v", entries)
	}
}

func TestListFilesSubdir(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	entries := mustList(t, b, "demo", "/etc")

	hostname := entryNamed(entries, "hostname")
	if hostname == nil || hostname.Dir {
		t.Fatalf("expected /etc/hostname file, got %+v", entries)
	}
}

func TestListFilesOnFileIsInvalid(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	_, err := b.ListFiles(ctx(), "demo", "/etc/hostname")
	if !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestListFilesGhostPathIs404(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	_, err := b.ListFiles(ctx(), "demo", "/no/such/dir")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestListFilesGhostInstanceIs404(t *testing.T) {
	_, err := New().ListFiles(ctx(), "ghost", "/")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestListFilesRelativePathIsInvalid(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	_, err := b.ListFiles(ctx(), "demo", "etc")
	if !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestPullFileContent(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	var buf bytes.Buffer
	if err := b.PullFile(ctx(), "demo", "/etc/hostname", &buf); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "demo") {
		t.Fatalf("hostname should contain the instance name, got %q", got)
	}
}

func TestPullFileDirectoryIsInvalid(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	err := b.PullFile(ctx(), "demo", "/etc", &bytes.Buffer{})
	if !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestPullFileGhostIs404(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	err := b.PullFile(ctx(), "demo", "/no/such", &bytes.Buffer{})
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPushFileRoundTrip(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	if err := b.PushFile(ctx(), "demo", "/root/notes.txt", strings.NewReader("hello")); err != nil {
		t.Fatalf("push: %v", err)
	}

	entries := mustList(t, b, "demo", "/root")
	if e := entryNamed(entries, "notes.txt"); e == nil || e.Dir {
		t.Fatalf("pushed file missing from /root: %+v", entries)
	}
	var buf bytes.Buffer
	if err := b.PullFile(ctx(), "demo", "/root/notes.txt", &buf); err != nil {
		t.Fatalf("pull back: %v", err)
	}
	if buf.String() != "hello" {
		t.Fatalf("round-trip content mismatch: %q", buf.String())
	}
}

func TestPushFileRelativePathIsInvalid(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	err := b.PushFile(ctx(), "demo", "notes.txt", strings.NewReader("x"))
	if !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestPushFileGhostInstanceIs404(t *testing.T) {
	err := New().PushFile(ctx(), "ghost", "/x", strings.NewReader("x"))
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
