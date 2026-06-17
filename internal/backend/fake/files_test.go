package fake

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
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

	if err := b.PushFile(ctx(), "demo", "/root/notes.txt", strings.NewReader("hello"), backend.FileWriteOptions{}); err != nil {
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
	err := b.PushFile(ctx(), "demo", "notes.txt", strings.NewReader("x"), backend.FileWriteOptions{})
	if !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestPushFileGhostInstanceIs404(t *testing.T) {
	err := New().PushFile(ctx(), "ghost", "/x", strings.NewReader("x"), backend.FileWriteOptions{})
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPushFileMissingParentIs404(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	err := b.PushFile(ctx(), "demo", "/no/such/file.txt", strings.NewReader("x"), backend.FileWriteOptions{})
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPushFileOptionsMetadataRoundTrip(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	opts := backend.FileWriteOptions{Mode: "0600", UID: 1000, GID: 1000}
	if err := b.PushFile(ctx(), "demo", "/root/secret", strings.NewReader("s3cret"), opts); err != nil {
		t.Fatalf("push: %v", err)
	}

	var buf bytes.Buffer
	info, err := b.PullFileInfo(ctx(), "demo", "/root/secret", &buf, 0)
	if err != nil {
		t.Fatalf("pull info: %v", err)
	}
	want := backend.FileInfo{Type: "file", Mode: "0600", UID: 1000, GID: 1000}
	if info != want {
		t.Fatalf("info mismatch: got %+v, want %+v", info, want)
	}
	if buf.String() != "s3cret" {
		t.Fatalf("content mismatch: %q", buf.String())
	}
}

func TestPushFileZeroOptionsKeepDefaults(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	if err := b.PushFile(ctx(), "demo", "/root/plain", strings.NewReader("x"), backend.FileWriteOptions{}); err != nil {
		t.Fatalf("push: %v", err)
	}
	info, err := b.PullFileInfo(ctx(), "demo", "/root/plain", &bytes.Buffer{}, 0)
	if err != nil {
		t.Fatalf("pull info: %v", err)
	}
	want := backend.FileInfo{Type: "file", Mode: "0644"}
	if info != want {
		t.Fatalf("info mismatch: got %+v, want %+v", info, want)
	}
}

func TestPushFileOverwritePreservesMetadata(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	require := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("push: %v", err)
		}
	}
	require(b.PushFile(ctx(), "demo", "/root/keep", strings.NewReader("v1"),
		backend.FileWriteOptions{Mode: "0600", UID: 1000, GID: 1000}))
	// Incus parity: the daemon ignores ownership/mode headers when
	// overwriting; only the content changes.
	require(b.PushFile(ctx(), "demo", "/root/keep", strings.NewReader("v2"),
		backend.FileWriteOptions{Mode: "0640"}))

	var buf bytes.Buffer
	info, err := b.PullFileInfo(ctx(), "demo", "/root/keep", &buf, 0)
	if err != nil {
		t.Fatalf("pull info: %v", err)
	}
	want := backend.FileInfo{Type: "file", Mode: "0600", UID: 1000, GID: 1000}
	if info != want || buf.String() != "v2" {
		t.Fatalf("got %+v / %q, want %+v / \"v2\"", info, buf.String(), want)
	}
}

func TestPushFileCleansPath(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	if err := b.PushFile(ctx(), "demo", "/root//./notes.txt", strings.NewReader("x"), backend.FileWriteOptions{}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if e := entryNamed(mustList(t, b, "demo", "/root"), "notes.txt"); e == nil {
		t.Fatalf("pushed file with uncleaned path missing from /root listing")
	}
}

func TestPullFileInfoLimitExceededIsInvalid(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	_, err := b.PullFileInfo(ctx(), "demo", "/etc/os-release", &bytes.Buffer{}, 4)
	if !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestPullFileInfoDirectoryReportsTypeWithoutContent(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	var buf bytes.Buffer
	info, err := b.PullFileInfo(ctx(), "demo", "/etc", &buf, 0)
	if err != nil {
		t.Fatalf("pull info: %v", err)
	}
	if info.Type != "directory" || buf.Len() != 0 {
		t.Fatalf("want directory with no content, got %+v (%d bytes)", info, buf.Len())
	}
}

func TestPullFileInfoSymlinkType(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	b.SeedSymlink("demo", "/etc/localtime")

	info, err := b.PullFileInfo(ctx(), "demo", "/etc/localtime", &bytes.Buffer{}, 0)
	if err != nil {
		t.Fatalf("pull info: %v", err)
	}
	if info.Type != "symlink" {
		t.Fatalf("want symlink, got %+v", info)
	}
}

func TestPullFileHeadWholeFile(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	if err := b.PushFile(ctx(), "demo", "/root/app.log", strings.NewReader("line1\nline2\n"),
		backend.FileWriteOptions{Mode: "0644"}); err != nil {
		t.Fatalf("push: %v", err)
	}

	var buf bytes.Buffer
	info, truncated, err := b.PullFileHead(ctx(), "demo", "/root/app.log", &buf, 1<<20)
	if err != nil {
		t.Fatalf("pull head: %v", err)
	}
	if truncated || buf.String() != "line1\nline2\n" || info.Type != "file" {
		t.Fatalf("got truncated=%v %q %+v, want full content", truncated, buf.String(), info)
	}
}

func TestPullFileHeadTruncatesLargeFile(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	if err := b.PushFile(ctx(), "demo", "/root/big.log", strings.NewReader("abcdefghij"),
		backend.FileWriteOptions{Mode: "0644"}); err != nil {
		t.Fatalf("push: %v", err)
	}

	var buf bytes.Buffer
	_, truncated, err := b.PullFileHead(ctx(), "demo", "/root/big.log", &buf, 4)
	if err != nil {
		t.Fatalf("pull head: %v", err)
	}
	if !truncated || buf.String() != "abcd" {
		t.Fatalf("got truncated=%v %q, want truncated head \"abcd\"", truncated, buf.String())
	}
}

func TestPullFileHeadExactLimitNotTruncated(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	if err := b.PushFile(ctx(), "demo", "/root/exact.log", strings.NewReader("abcd"),
		backend.FileWriteOptions{Mode: "0644"}); err != nil {
		t.Fatalf("push: %v", err)
	}

	var buf bytes.Buffer
	_, truncated, err := b.PullFileHead(ctx(), "demo", "/root/exact.log", &buf, 4)
	if err != nil {
		t.Fatalf("pull head: %v", err)
	}
	if truncated || buf.String() != "abcd" {
		t.Fatalf("a file exactly at the limit is not truncated: got truncated=%v %q", truncated, buf.String())
	}
}

func TestPullFileHeadDirectoryReportsTypeWithoutContent(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	var buf bytes.Buffer
	info, truncated, err := b.PullFileHead(ctx(), "demo", "/etc", &buf, 1<<20)
	if err != nil {
		t.Fatalf("pull head: %v", err)
	}
	if info.Type != "directory" || buf.Len() != 0 || truncated {
		t.Fatalf("want directory with no content, got %+v (%d bytes, truncated=%v)", info, buf.Len(), truncated)
	}
}

func TestPullFileHeadSymlinkReportsTypeWithoutContent(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	b.SeedSymlink("demo", "/etc/localtime")

	var buf bytes.Buffer
	info, truncated, err := b.PullFileHead(ctx(), "demo", "/etc/localtime", &buf, 1<<20)
	if err != nil {
		t.Fatalf("pull head: %v", err)
	}
	if info.Type != "symlink" || buf.Len() != 0 || truncated {
		t.Fatalf("want symlink with no content, got %+v (%d bytes, truncated=%v)", info, buf.Len(), truncated)
	}
}

func TestDeleteFileRemovesFile(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	if err := b.DeleteFile(ctx(), "demo", "/etc/hostname"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if e := entryNamed(mustList(t, b, "demo", "/etc"), "hostname"); e != nil {
		t.Fatalf("hostname should be gone, got %+v", e)
	}
}

func TestDeleteFileEmptyDir(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	if err := b.MakeDirectory(ctx(), "demo", "/empty"); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := b.DeleteFile(ctx(), "demo", "/empty"); err != nil {
		t.Fatalf("delete empty dir: %v", err)
	}
	if e := entryNamed(mustList(t, b, "demo", "/"), "empty"); e != nil {
		t.Fatalf("empty dir should be gone, got %+v", e)
	}
}

func TestDeleteFileNonEmptyDirIsInvalid(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	err := b.DeleteFile(ctx(), "demo", "/etc")
	if !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestDeleteFileRootIsInvalid(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	err := b.DeleteFile(ctx(), "demo", "/")
	if !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestDeleteFileMissingIs404(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	err := b.DeleteFile(ctx(), "demo", "/no/such")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestMakeDirectoryListsAsEmptyDir(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")

	if err := b.MakeDirectory(ctx(), "demo", "/data"); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	e := entryNamed(mustList(t, b, "demo", "/"), "data")
	if e == nil || !e.Dir {
		t.Fatalf("expected /data directory entry, got %+v", e)
	}
	entries := mustList(t, b, "demo", "/data")
	if len(entries) != 0 {
		t.Fatalf("new directory should be empty, got %+v", entries)
	}
}

func TestMakeDirectoryExistingIsConflict(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	err := b.MakeDirectory(ctx(), "demo", "/etc")
	if !errors.Is(err, backend.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestMakeDirectoryMissingParentIs404(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo")
	err := b.MakeDirectory(ctx(), "demo", "/no/such")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
