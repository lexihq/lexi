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
	if strings.Contains(html, `hx-post="/instances/demo/snapshots"`) {
		t.Fatalf("list row must not render snapshot creation without a snapshot name: %q", html)
	}
}

func TestInstancePageSummaryTabRendersDetails(t *testing.T) {
	html := render(t, InstancePage(testCaps(), backend.Instance{Name: "demo", Status: "Running", Image: "debian/12"}, []backend.Snapshot{{Name: "snap0"}}, "summary"))

	assertContains(t, html, "demo")
	assertContains(t, html, "Running")
	assertContains(t, html, "Details")
	assertContains(t, html, "debian/12")
	// The Snapshots table lives behind its own tab, not the default Summary.
	if strings.Contains(html, `hx-post="/instances/demo/snapshots"`) {
		t.Fatalf("summary tab must not render snapshot controls: %q", html)
	}
}

func TestInstancePageSnapshotsTabRendersControls(t *testing.T) {
	html := render(t, InstancePage(testCaps(), backend.Instance{Name: "demo", Status: "Running"}, []backend.Snapshot{{Name: "snap0"}}, "snapshots"))

	assertContains(t, html, "snap0")
	assertContains(t, html, `hx-post="/instances/demo/snapshots"`)
	assertContains(t, html, `hx-post="/instances/demo/snapshots/snap0/restore"`)
}

func TestInstancePageGatesDisabledTabsToSummary(t *testing.T) {
	// A direct ?tab= URL for a capability the backend lacks must fall back to
	// Summary, never emitting the panel's poller/controls. We assert on the
	// panel's wrapper attributes (the load-bearing trigger), not on lazy-loaded
	// panel text which InstanceBody never renders inline.
	allOn := backend.Capabilities{Snapshots: true, Metrics: true, Console: true, Limits: true}
	cases := []struct {
		name        string
		tab         string
		caps        backend.Capabilities
		mustNotHave string
	}{
		{"metrics off", "metrics", capsWithout(allOn, "metrics"), `hx-get="/instances/demo/metrics"`},
		{"logs off", "logs", capsWithout(allOn, "logs"), `hx-get="/instances/demo/logs"`},
		{"snapshots off", "snapshots", capsWithout(allOn, "snapshots"), `hx-post="/instances/demo/snapshots"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html := render(t, InstancePage(tc.caps, backend.Instance{Name: "demo", Status: "Running", Image: "debian/12"}, []backend.Snapshot{{Name: "snap0"}}, tc.tab))

			assertContains(t, html, "Details") // summary fallback
			if strings.Contains(html, tc.mustNotHave) {
				t.Fatalf("disabled %s tab must fall back to summary, found %q in %q", tc.tab, tc.mustNotHave, html)
			}
		})
	}
}

func TestInstancePageDefaultTabHighlightsSummary(t *testing.T) {
	// The bare detail URL passes an empty tab; it must resolve to Summary and
	// mark the Summary tab (not another) active.
	html := render(t, InstancePage(testCaps(), backend.Instance{Name: "demo", Status: "Running"}, nil, ""))

	assertActiveTab(t, html, "Summary")
}

func TestSidebarInstancesRendersStatusDotsAndActive(t *testing.T) {
	html := render(t, SidebarInstances([]backend.Instance{
		{Name: "running-one", Status: "Running"},
		{Name: "stopped-one", Status: "Stopped"},
	}, "running-one"))

	assertContains(t, html, "running-one")
	assertContains(t, html, "stopped-one")
	assertContains(t, html, "bg-green-500")        // running status dot
	assertContains(t, html, "bg-muted-foreground") // stopped status dot
	assertContains(t, html, "bg-accent")           // active highlight on running-one
}

func TestCreatePageRendersImageForm(t *testing.T) {
	html := render(t, CreatePage(testCaps(), []backend.Image{{
		Alias:       "debian/12",
		Description: "Debian 12",
		Fingerprint: "debian-fingerprint",
		Type:        "container",
	}}))

	assertContains(t, html, `action="/instances"`)
	assertContains(t, html, `name="name"`)
	assertContains(t, html, `value="debian-fingerprint"`)
	assertContains(t, html, "Debian 12")
	assertContains(t, html, "container")
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

// capsWithout returns caps with a single capability disabled, so a gating test
// can flip exactly the one it exercises.
func capsWithout(caps backend.Capabilities, feature string) backend.Capabilities {
	switch feature {
	case "metrics":
		caps.Metrics = false
	case "logs", "console":
		caps.Console = false
	case "snapshots":
		caps.Snapshots = false
	}
	return caps
}

// assertActiveTab verifies the tab anchor with the given label carries the
// active-tab styling (border-primary), proving it — and not a sibling — is
// highlighted.
func assertActiveTab(t *testing.T, html, label string) {
	t.Helper()
	close := ">" + label + "<"
	idx := strings.Index(html, close)
	if idx < 0 {
		t.Fatalf("tab %q not found in %q", label, html)
	}
	open := strings.LastIndex(html[:idx], "<a")
	if open < 0 {
		t.Fatalf("no anchor opening for tab %q", label)
	}
	if tag := html[open:idx]; !strings.Contains(tag, "border-primary") {
		t.Fatalf("tab %q is not active (missing border-primary): %q", label, tag)
	}
}
