//go:build integration

package incus

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// waitForConfigKey polls the server config until key has (or no longer has,
// for want == "") the wanted value. Incus 6.0.x applies config writes against
// a lazily refreshed in-memory cache, so both reads-after-write and
// write-after-write need settling time.
func waitForConfigKey(t *testing.T, b *incusBackend, key, want string) map[string]string {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(15 * time.Second)
	for {
		cfg, _, err := b.GetServerConfig(ctx)
		require.NoError(t, err)
		if cfg[key] == want {
			return cfg
		}
		require.False(t, time.Now().After(deadline),
			"config key %q never reached %q (last: %q)", key, want, cfg[key])
		time.Sleep(100 * time.Millisecond)
	}
}

// TestServerAdminRoundTrip exercises the Server section reads against the live
// daemon and round-trips a config change through the user.* namespace,
// restoring the original config afterwards.
//
// The stale-version → ErrConflict path is deliberately NOT asserted here: the
// daemon (Incus 6.0.x) refreshes the etag render of GET /1.0 lazily, so a
// stale If-Match can be accepted for an unbounded window after a write. The
// conditional-write contract is pinned by the stub test
// (TestUpdateServerConfigEtagRaceIsConflict), the fake, and the server-layer
// 409 test; daemon-side rejection was verified manually once the render
// settled.
func TestServerAdminRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()

	overview, err := b.GetServerOverview(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, overview.ServerVersion)
	require.Positive(t, overview.CPUThreads)
	require.Positive(t, overview.MemoryTotal)

	original, version, err := b.GetServerConfig(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, version)
	require.Empty(t, original["user.lxcon-test"], "leftover test key; unset user.lxcon-test and re-run")

	mod := maps.Clone(original)
	if mod == nil {
		mod = map[string]string{}
	}
	mod["user.lxcon-test"] = "1"
	// Unconditional write: the version token is exercised by stub/fake tests
	// (see the comment above).
	require.NoError(t, b.UpdateServerConfig(ctx, mod, ""))
	t.Cleanup(func() {
		// The daemon diffs writes against the same lazy cache, so a single
		// restore can be silently swallowed right after another write; retry
		// until the key is really gone.
		for range 5 {
			if err := b.UpdateServerConfig(ctx, original, ""); err != nil {
				t.Errorf("restore server config: %v", err)
				return
			}
			cfg, _, err := b.GetServerConfig(ctx)
			if err == nil && cfg["user.lxcon-test"] == "" {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Error("restore never took effect; unset user.lxcon-test manually")
	})

	waitForConfigKey(t, b, "user.lxcon-test", "1")

	_, err = b.ListCertificates(ctx)
	require.NoError(t, err)
	_, err = b.ListWarnings(ctx)
	require.NoError(t, err)
}
