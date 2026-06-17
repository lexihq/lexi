//go:build integration

package incus

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/require"
)

// TestFilePushPullRoundTrip pushes a file into a (stopped — the file API works
// without a running instance) container, lists its directory, and pulls the
// content back.
func TestFilePushPullRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("file")
	t.Cleanup(func() { cleanupInstance(t, b, name) })
	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	const target = "/root/lexi-roundtrip.txt"
	content := "lexi file transfer\n"
	require.NoError(t, b.PushFile(ctx, name, target, strings.NewReader(content), backend.FileWriteOptions{}))

	entries, err := b.ListFiles(ctx, name, "/root")
	require.NoError(t, err)
	var found bool
	for _, e := range entries {
		if e.Name == "lexi-roundtrip.txt" && !e.Dir {
			found = true
		}
	}
	require.True(t, found, "pushed file missing from listing: %+v", entries)

	var buf bytes.Buffer
	require.NoError(t, b.PullFile(ctx, name, target, &buf))
	require.Equal(t, content, buf.String())
}

// TestFileMetadataRoundTrip pushes a file with non-default ownership and mode
// and verifies PullFileInfo reports them, then re-pushes with the same options
// (as the in-browser editor does) and checks the metadata survives.
func TestFileMetadataRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()
	name := uniqueName("filemeta")
	t.Cleanup(func() { cleanupInstance(t, b, name) })
	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	const target = "/root/lexi-meta.txt"
	opts := backend.FileWriteOptions{Mode: "0600", UID: 1000, GID: 1000}
	require.NoError(t, b.PushFile(ctx, name, target, strings.NewReader("v1\n"), opts))

	var buf bytes.Buffer
	info, err := b.PullFileInfo(ctx, name, target, &buf, 1<<20)
	require.NoError(t, err)
	require.Equal(t, "v1\n", buf.String())
	require.Equal(t, backend.FileInfo{Type: "file", Mode: "0600", UID: 1000, GID: 1000}, info)

	// Edit-save: push new content with the options captured at read time.
	require.NoError(t, b.PushFile(ctx, name, target, strings.NewReader("v2\n"),
		backend.FileWriteOptions{Mode: info.Mode, UID: info.UID, GID: info.GID}))
	buf.Reset()
	info, err = b.PullFileInfo(ctx, name, target, &buf, 1<<20)
	require.NoError(t, err)
	require.Equal(t, "v2\n", buf.String())
	require.Equal(t, backend.FileInfo{Type: "file", Mode: "0600", UID: 1000, GID: 1000}, info)

	// A limit smaller than the file refuses instead of truncating.
	_, err = b.PullFileInfo(ctx, name, target, &bytes.Buffer{}, 1)
	require.ErrorIs(t, err, backend.ErrInvalid)

	// PullFileHead truncates to the limit and reports it (the read-only viewer
	// path); a generous limit returns the whole file untruncated.
	buf.Reset()
	_, truncated, err := b.PullFileHead(ctx, name, target, &buf, 1)
	require.NoError(t, err)
	require.True(t, truncated)
	require.Equal(t, "v", buf.String())
	buf.Reset()
	_, truncated, err = b.PullFileHead(ctx, name, target, &buf, 1<<20)
	require.NoError(t, err)
	require.False(t, truncated)
	require.Equal(t, "v2\n", buf.String())

	// Overwriting with different options keeps the existing metadata — the
	// daemon ignores ownership/mode headers on overwrite (fake mirrors this).
	require.NoError(t, b.PushFile(ctx, name, target, strings.NewReader("v3\n"),
		backend.FileWriteOptions{Mode: "0640"}))
	info, err = b.PullFileInfo(ctx, name, target, &bytes.Buffer{}, 1<<20)
	require.NoError(t, err)
	require.Equal(t, backend.FileInfo{Type: "file", Mode: "0600", UID: 1000, GID: 1000}, info)
}

// TestFileMkdirDeleteRoundTrip creates a directory, pushes a file into it,
// deletes the file, then deletes the (now empty) directory.
func TestFileMkdirDeleteRoundTrip(t *testing.T) {
	b := newBackend(t)
	caps := b.Capabilities(context.Background())
	if !caps.FileMkdir || !caps.FileDelete {
		t.Skipf("daemon lacks file extensions: mkdir=%v delete=%v", caps.FileMkdir, caps.FileDelete)
	}
	ctx := context.Background()
	name := uniqueName("filedir")
	t.Cleanup(func() { cleanupInstance(t, b, name) })
	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	const dir = "/root/lexi-dir"
	require.NoError(t, b.MakeDirectory(ctx, name, dir))

	// Re-creating must conflict (the daemon would silently succeed; the
	// driver pre-checks).
	require.ErrorIs(t, b.MakeDirectory(ctx, name, dir), backend.ErrConflict)

	const target = dir + "/inner.txt"
	require.NoError(t, b.PushFile(ctx, name, target, strings.NewReader("inner\n"), backend.FileWriteOptions{}))

	// Non-empty directory delete is a user error, not a 500.
	require.ErrorIs(t, b.DeleteFile(ctx, name, dir), backend.ErrInvalid)

	require.NoError(t, b.DeleteFile(ctx, name, target))
	require.NoError(t, b.DeleteFile(ctx, name, dir))

	entries, err := b.ListFiles(ctx, name, "/root")
	require.NoError(t, err)
	for _, e := range entries {
		require.NotEqual(t, "lexi-dir", e.Name, "directory should be gone")
	}
}
