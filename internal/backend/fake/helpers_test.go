package fake

import (
	"context"
	"testing"

	"github.com/adam/lxcon/internal/backend"
)

// Shared test helpers for the fake package, used across the per-feature
// *_test.go files.

func ctx() context.Context { return context.Background() }

func mustCreate(t *testing.T, b *Fake, name string) {
	t.Helper()
	if err := b.CreateInstance(ctx(), backend.CreateOptions{Name: name, Image: "debian/12"}); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
}
