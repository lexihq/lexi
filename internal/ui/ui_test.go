package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/adam/lxcon/internal/backend"

	"github.com/a-h/templ"
)

func TestInstancesPageRendersListAndActions(t *testing.T) {
	html := render(t, InstancesPage(testCaps(), []backend.Instance{{Name: "demo", Status: "Stopped", IPv4: []string{"10.0.3.12"}, Snapshots: 2}}))

	assertContains(t, html, "fake backend")
	assertContains(t, html, "demo")
	assertContains(t, html, "Stopped")
	assertContains(t, html, "10.0.3.12")
	assertContains(t, html, `hx-post="/instances/demo/start"`)
	assertContains(t, html, `hx-post="/instances/demo/delete"`)
}

func TestInstancePageRendersSnapshotControls(t *testing.T) {
	html := render(t, InstancePage(testCaps(), backend.Instance{Name: "demo", Status: "Running"}, []backend.Snapshot{{Name: "snap0"}}))

	assertContains(t, html, "demo")
	assertContains(t, html, "Running")
	assertContains(t, html, "snap0")
	assertContains(t, html, `hx-post="/instances/demo/snapshots"`)
	assertContains(t, html, `hx-post="/instances/demo/snapshots/snap0/restore"`)
}

func TestCreatePageRendersImageForm(t *testing.T) {
	html := render(t, CreatePage(testCaps(), []backend.Image{{Alias: "debian/12", Description: "Debian 12"}}))

	assertContains(t, html, `action="/instances"`)
	assertContains(t, html, `name="name"`)
	assertContains(t, html, `value="debian/12"`)
	assertContains(t, html, "Debian 12")
}

func TestInstanceRowCanHideUnsupportedActions(t *testing.T) {
	caps := testCaps()
	caps.Snapshots = false
	caps.Clone = false
	html := render(t, InstanceRow(caps, backend.Instance{Name: "demo", Status: "Stopped"}))

	if strings.Contains(html, "/clone") || strings.Contains(html, "/snapshots") {
		t.Fatalf("expected unsupported actions hidden, got %q", html)
	}
}

func render(t *testing.T, component templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := component.Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func testCaps() backend.Capabilities {
	return backend.Capabilities{Tier: backend.TierFake, ServerInfo: "fake backend", Snapshots: true, Clone: true}
}

func assertContains(t *testing.T, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Fatalf("expected %q to contain %q", s, want)
	}
}
