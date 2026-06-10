//go:build integration

package incus

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/adam/lxcon/internal/backend"
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

	const target = "/root/lxcon-roundtrip.txt"
	content := "lxcon file transfer\n"
	require.NoError(t, b.PushFile(ctx, name, target, strings.NewReader(content)))

	entries, err := b.ListFiles(ctx, name, "/root")
	require.NoError(t, err)
	var found bool
	for _, e := range entries {
		if e.Name == "lxcon-roundtrip.txt" && !e.Dir {
			found = true
		}
	}
	require.True(t, found, "pushed file missing from listing: %+v", entries)

	var buf bytes.Buffer
	require.NoError(t, b.PullFile(ctx, name, target, &buf))
	require.Equal(t, content, buf.String())
}

// TestFileMkdirDeleteRoundTrip creates a directory, pushes a file into it,
// deletes the file, then deletes the (now empty) directory.
func TestFileMkdirDeleteRoundTrip(t *testing.T) {
	b := newBackend(t)
	caps := b.Capabilities()
	if !caps.FileMkdir || !caps.FileDelete {
		t.Skipf("daemon lacks file extensions: mkdir=%v delete=%v", caps.FileMkdir, caps.FileDelete)
	}
	ctx := context.Background()
	name := uniqueName("filedir")
	t.Cleanup(func() { cleanupInstance(t, b, name) })
	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	const dir = "/root/lxcon-dir"
	require.NoError(t, b.MakeDirectory(ctx, name, dir))

	const target = dir + "/inner.txt"
	require.NoError(t, b.PushFile(ctx, name, target, strings.NewReader("inner\n")))

	// Non-empty directory delete must fail before the file is removed.
	require.Error(t, b.DeleteFile(ctx, name, dir))

	require.NoError(t, b.DeleteFile(ctx, name, target))
	require.NoError(t, b.DeleteFile(ctx, name, dir))

	entries, err := b.ListFiles(ctx, name, "/root")
	require.NoError(t, err)
	for _, e := range entries {
		require.NotEqual(t, "lxcon-dir", e.Name, "directory should be gone")
	}
}
