//go:build integration

package incus

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"maps"
	"math/big"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
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

// TestAddCertificateRoundTrip adds a generated self-signed certificate to the
// trust store, sees it listed, then removes it via the raw client.
func TestAddCertificateRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "lxcon-integration"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	sum := sha256.Sum256(der)
	fingerprint := hex.EncodeToString(sum[:])
	pemData := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	t.Cleanup(func() {
		if err := b.srv.DeleteCertificate(fingerprint); err != nil {
			t.Logf("cleanup certificate %s: %v", fingerprint, err)
		}
	})

	name := uniqueName("lxcon-cert")
	require.NoError(t, b.AddCertificate(ctx, name, "metrics", pemData))

	certs, err := b.ListCertificates(ctx)
	require.NoError(t, err)
	var found bool
	for _, c := range certs {
		if c.Fingerprint == fingerprint {
			found = true
			require.Equal(t, name, c.Name)
			require.Equal(t, "metrics", c.Type)
		}
	}
	require.True(t, found, "added certificate not listed")

	// Garbage is rejected locally; duplicates conflict at the daemon.
	require.ErrorIs(t, b.AddCertificate(ctx, "junk", "client", "garbage"), backend.ErrInvalid)
	require.ErrorIs(t, b.AddCertificate(ctx, name, "metrics", pemData), backend.ErrConflict)
}

// TestAcknowledgeWarningRoundTrip acknowledges the newest daemon warning and
// verifies the status flip; skips on hosts with a clean warning list.
func TestAcknowledgeWarningRoundTrip(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()

	warnings, err := b.ListWarnings(ctx)
	require.NoError(t, err)
	if len(warnings) == 0 {
		t.Skip("no warnings on this host")
	}
	target := warnings[0]

	require.NoError(t, b.AcknowledgeWarning(ctx, target.UUID))

	after, err := b.ListWarnings(ctx)
	require.NoError(t, err)
	for _, w := range after {
		if w.UUID == target.UUID {
			require.Equal(t, "acknowledged", w.Status)
			return
		}
	}
	t.Fatalf("acknowledged warning %s vanished", target.UUID)
}
