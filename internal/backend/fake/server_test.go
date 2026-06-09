package fake

import (
	"errors"
	"testing"

	"github.com/adam/lxcon/internal/backend"
)

func TestGetServerOverviewStatic(t *testing.T) {
	o, err := New().GetServerOverview(ctx())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if o.ServerVersion == "" || o.CPUThreads == 0 || o.MemoryTotal == 0 {
		t.Fatalf("overview missing fields: %+v", o)
	}
}

func TestServerConfigRoundTrip(t *testing.T) {
	b := New()
	cfg, version, err := b.GetServerConfig(ctx())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg["core.https_address"] == "" {
		t.Fatalf("expected seeded core.https_address, got %+v", cfg)
	}
	if version == "" {
		t.Fatal("expected a non-empty config version token")
	}

	if err := b.UpdateServerConfig(ctx(), map[string]string{"user.x": "1"}, version); err != nil {
		t.Fatalf("update config: %v", err)
	}
	cfg, _, err = b.GetServerConfig(ctx())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg["user.x"] != "1" {
		t.Errorf("updated key missing: %+v", cfg)
	}
	if _, ok := cfg["core.https_address"]; ok {
		t.Errorf("replace semantics: dropped key survived: %+v", cfg)
	}
}

func TestServerConfigStaleVersionIsConflict(t *testing.T) {
	b := New()
	_, version, err := b.GetServerConfig(ctx())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	// A concurrent writer lands first; the held version goes stale.
	if err := b.UpdateServerConfig(ctx(), map[string]string{"user.first": "1"}, version); err != nil {
		t.Fatalf("first update: %v", err)
	}
	err = b.UpdateServerConfig(ctx(), map[string]string{"user.second": "2"}, version)
	if !errors.Is(err, backend.ErrConflict) {
		t.Fatalf("stale version must conflict, got %v", err)
	}
	cfg, _, err := b.GetServerConfig(ctx())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg["user.first"] != "1" {
		t.Errorf("first writer's config must survive: %+v", cfg)
	}
}

func TestServerConfigEmptyVersionIsUnconditional(t *testing.T) {
	// Mirrors the Incus client: an empty etag sends no If-Match.
	b := New()
	if err := b.UpdateServerConfig(ctx(), map[string]string{"user.x": "1"}, ""); err != nil {
		t.Fatalf("unconditional update: %v", err)
	}
}

func TestListCertificatesSeeded(t *testing.T) {
	certs, err := New().ListCertificates(ctx())
	if err != nil {
		t.Fatalf("list certificates: %v", err)
	}
	if len(certs) == 0 || certs[0].Fingerprint == "" {
		t.Fatalf("expected a seeded certificate, got %+v", certs)
	}
}

func TestWarningsSeededAndDelete(t *testing.T) {
	b := New()
	warnings, err := b.ListWarnings(ctx())
	if err != nil {
		t.Fatalf("list warnings: %v", err)
	}
	if len(warnings) < 2 {
		t.Fatalf("expected seeded warnings, got %+v", warnings)
	}
	if warnings[0].LastSeenAt.Before(warnings[1].LastSeenAt) {
		t.Errorf("warnings must sort newest first: %+v", warnings)
	}

	if err := b.DeleteWarning(ctx(), warnings[0].UUID); err != nil {
		t.Fatalf("delete warning: %v", err)
	}
	after, err := b.ListWarnings(ctx())
	if err != nil {
		t.Fatalf("list warnings: %v", err)
	}
	if len(after) != len(warnings)-1 {
		t.Fatalf("expected %d warnings after delete, got %d", len(warnings)-1, len(after))
	}
}

func TestDeleteWarningGhostIs404(t *testing.T) {
	err := New().DeleteWarning(ctx(), "no-such-uuid")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
