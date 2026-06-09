//go:build integration

package incus

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
)

// testImage is small and fast to pull, keeping integration runs cheap.
const testImage = "alpine/edge"

func newBackend(t *testing.T) *incusBackend {
	t.Helper()
	b, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("lxcon-it-%s-%d", prefix, time.Now().UnixNano()%1_000_000)
}

func listed(list []backend.Instance, name string) bool {
	for _, i := range list {
		if i.Name == name {
			return true
		}
	}
	return false
}

func cleanupInstance(t *testing.T, b *incusBackend, name string) {
	t.Helper()
	if err := b.DeleteInstance(context.Background(), name); err != nil && !errors.Is(err, backend.ErrNotFound) {
		t.Errorf("cleanup instance %q: %v", name, err)
	}
}

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
