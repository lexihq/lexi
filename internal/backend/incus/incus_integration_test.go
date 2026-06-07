//go:build integration

// Integration tests run against a real Incus daemon via the current `incus` CLI
// remote (see Makefile target test-integration). They are excluded from the
// default `go test ./...` build.
package incus

import (
	"testing"

	"github.com/adam/lxcon/internal/backend"
)

func TestConnect(t *testing.T) {
	b, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	caps := b.Capabilities()
	if caps.Tier != backend.TierIncus {
		t.Fatalf("want tier %q, got %q", backend.TierIncus, caps.Tier)
	}
	if caps.ServerInfo == "" {
		t.Fatal("ServerInfo should report the server version")
	}
	if !caps.Snapshots || !caps.Clone {
		t.Fatalf("incus tier should advertise snapshots and clone: %+v", caps)
	}
	t.Logf("connected: %s", caps.ServerInfo)
}
