//go:build integration

package incus

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestServerAdminRoundTrip exercises the Server section reads against the live
// daemon and round-trips a config change through the user.* namespace,
// restoring the original config afterwards.
func TestServerAdminRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()

	overview, err := b.GetServerOverview(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, overview.ServerVersion)
	require.Positive(t, overview.CPUThreads)
	require.Positive(t, overview.MemoryTotal)

	original, err := b.GetServerConfig(ctx)
	require.NoError(t, err)

	mod := maps.Clone(original)
	if mod == nil {
		mod = map[string]string{}
	}
	mod["user.lxcon-test"] = "1"
	require.NoError(t, b.UpdateServerConfig(ctx, mod))
	t.Cleanup(func() {
		if err := b.UpdateServerConfig(ctx, original); err != nil {
			t.Errorf("restore server config: %v", err)
		}
	})

	got, err := b.GetServerConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, "1", got["user.lxcon-test"])

	_, err = b.ListCertificates(ctx)
	require.NoError(t, err)
	_, err = b.ListWarnings(ctx)
	require.NoError(t, err)
}
