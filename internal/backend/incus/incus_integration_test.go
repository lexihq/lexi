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
	// Retry briefly: on a memory-tight host the daemon can get OOM-killed by a
	// previous test's load and takes a few seconds to come back. Without the
	// retry one daemon restart cascades into instant connect-EOF failures for
	// every remaining test, hiding the real culprit.
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(2 * time.Second)
		}
		b, err := New()
		if err == nil {
			return b
		}
		lastErr = err
	}
	t.Fatalf("New: %v", lastErr)
	return nil
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
	b := newBackend(t)
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
