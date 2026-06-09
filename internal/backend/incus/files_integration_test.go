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
