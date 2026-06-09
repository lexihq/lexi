package incus

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	incusclient "github.com/lxc/incus/v6/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func filesBackend() (*incusBackend, *instanceServerStub) {
	srv := &instanceServerStub{files: map[string]*fileStub{
		"/etc": {resp: incusclient.InstanceFileResponse{
			Type:    "directory",
			Mode:    0o755,
			Entries: []string{"hostname", "ssl", "broken-link"},
		}},
		"/etc/hostname": {content: "demo\n", resp: incusclient.InstanceFileResponse{Type: "file", Mode: 0o644}},
		"/etc/ssl":      {resp: incusclient.InstanceFileResponse{Type: "directory", Mode: 0o755}},
		// "/etc/broken-link" intentionally absent: stat fails.
	}}
	return &incusBackend{srv: srv}, srv
}

func TestListFilesStatsEntries(t *testing.T) {
	b, _ := filesBackend()

	got, err := b.ListFiles(context.Background(), "demo", "/etc")

	require.NoError(t, err)
	require.Len(t, got, 3)
	// Directories first, then files, alphabetical within each group.
	assert.Equal(t, backend.FileEntry{Name: "ssl", Dir: true, Mode: "0755"}, got[0])
	assert.Equal(t, backend.FileEntry{Name: "broken-link", Dir: false, Mode: ""}, got[1])
	assert.Equal(t, backend.FileEntry{Name: "hostname", Dir: false, Mode: "0644"}, got[2])
}

func TestListFilesOnFileIsInvalid(t *testing.T) {
	b, _ := filesBackend()
	_, err := b.ListFiles(context.Background(), "demo", "/etc/hostname")
	require.ErrorIs(t, err, backend.ErrInvalid)
}

func TestListFilesGhostPathIsNotFound(t *testing.T) {
	b, _ := filesBackend()
	_, err := b.ListFiles(context.Background(), "demo", "/nope")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestPullFileStreamsContent(t *testing.T) {
	b, _ := filesBackend()

	var buf bytes.Buffer
	require.NoError(t, b.PullFile(context.Background(), "demo", "/etc/hostname", &buf))

	assert.Equal(t, "demo\n", buf.String())
}

func TestPullFileDirectoryIsInvalid(t *testing.T) {
	b, _ := filesBackend()
	err := b.PullFile(context.Background(), "demo", "/etc", &bytes.Buffer{})
	require.ErrorIs(t, err, backend.ErrInvalid)
}

func TestPushFileSendsContentAndDefaults(t *testing.T) {
	b, srv := filesBackend()

	require.NoError(t, b.PushFile(context.Background(), "demo", "/root/notes.txt", strings.NewReader("hello")))

	assert.Equal(t, "/root/notes.txt", srv.createdPath)
	require.NotNil(t, srv.createdFile)
	content, err := io.ReadAll(srv.createdFile.Content)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))
	assert.Equal(t, "file", srv.createdFile.Type)
	assert.Equal(t, "overwrite", srv.createdFile.WriteMode)
	assert.Equal(t, 0o644, srv.createdFile.Mode)
}
