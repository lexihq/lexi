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
	html := render(t, InstancePage(testCaps(), backend.Instance{Name: "demo", Status: "Running", Image: "debian/12"}, []backend.Snapshot{{Name: "snap0"}}, nil, "summary"))

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
	html := render(t, InstancePage(testCaps(), backend.Instance{Name: "demo", Status: "Running"}, []backend.Snapshot{{Name: "snap0"}}, nil, "snapshots"))

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
			html := render(t, InstancePage(tc.caps, backend.Instance{Name: "demo", Status: "Running", Image: "debian/12"}, []backend.Snapshot{{Name: "snap0"}}, nil, tc.tab))

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
	html := render(t, InstancePage(testCaps(), backend.Instance{Name: "demo", Status: "Running"}, nil, nil, ""))

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

func TestConsolePageOptsOutOfBoost(t *testing.T) {
	// The console page must not be hx-boosted: leaving it has to be a real
	// navigation so the browser unloads the document and closes the terminal
	// WebSocket. Normal pages keep the in-place SPA shell.
	console := render(t, ConsolePage(testCaps(), backend.Instance{Name: "demo"}))
	if strings.Contains(console, `hx-boost="true"`) {
		t.Fatalf("console page must opt out of hx-boost, got %q", console)
	}

	list := render(t, InstancesPage(testCaps(), nil))
	assertContains(t, list, `hx-boost="true"`)
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

func TestInstanceRowShowsRestartAlwaysAndPauseResumeByStatus(t *testing.T) {
	caps := backend.Capabilities{Pause: true}

	running := render(t, InstanceRow(caps, backend.Instance{Name: "demo", Status: "Running"}))
	assertContains(t, running, "/instances/demo/restart")
	assertContains(t, running, "/instances/demo/pause")
	if strings.Contains(running, "/instances/demo/resume") {
		t.Fatalf("running instance must not show Resume, got %q", running)
	}

	frozen := render(t, InstanceRow(caps, backend.Instance{Name: "demo", Status: "Frozen"}))
	assertContains(t, frozen, "/instances/demo/resume")
	if strings.Contains(frozen, "/instances/demo/pause") {
		t.Fatalf("frozen instance must not show Pause, got %q", frozen)
	}

	noPause := render(t, InstanceRow(backend.Capabilities{}, backend.Instance{Name: "demo", Status: "Running"}))
	assertContains(t, noPause, "/instances/demo/restart")
	if strings.Contains(noPause, "/instances/demo/pause") || strings.Contains(noPause, "/instances/demo/resume") {
		t.Fatalf("pause/resume must be hidden without the Pause capability, got %q", noPause)
	}
}

func TestSidebarGatesProfilesLinkOnCapability(t *testing.T) {
	withProfiles := render(t, Sidebar(backend.Capabilities{Profiles: true}, Nav{Section: NavProfiles}))
	assertContains(t, withProfiles, "/profiles")
	assertContains(t, withProfiles, "Profiles")

	without := render(t, Sidebar(backend.Capabilities{}, Nav{Section: NavInstances}))
	if strings.Contains(without, "/profiles") {
		t.Fatalf("profiles link must be hidden without the capability, got %q", without)
	}
}

func TestSidebarGatesNetworksLinkOnCapability(t *testing.T) {
	with := render(t, Sidebar(backend.Capabilities{Networks: true}, Nav{Section: NavNetworks}))
	assertContains(t, with, "/networks")
	assertContains(t, with, "Networks")

	without := render(t, Sidebar(backend.Capabilities{}, Nav{Section: NavInstances}))
	if strings.Contains(without, `href="/networks"`) {
		t.Fatalf("networks link must be hidden without the capability, got %q", without)
	}
}

func TestNetworksTableShowsManagedBadgeAndDeleteOnlyWhenDeletable(t *testing.T) {
	html := render(t, NetworksTable([]backend.Network{
		{Name: "incusbr0", Type: "bridge", Managed: true},                       // free → deletable
		{Name: "eth0", Type: "physical", Managed: false},                        // unmanaged → no delete
		{Name: "busy", Type: "bridge", Managed: true, UsedBy: []string{"demo"}}, // in use → no delete
	}))
	assertContains(t, html, `hx-post="/networks/incusbr0/delete"`)
	if strings.Contains(html, "/networks/eth0/delete") {
		t.Fatalf("unmanaged network must not have a delete button: %q", html)
	}
	if strings.Contains(html, "/networks/busy/delete") {
		t.Fatalf("in-use network must not have a delete button: %q", html)
	}
}

func TestNetworkCreatePageRendersTypeOptions(t *testing.T) {
	html := render(t, NetworkCreatePage(backend.Capabilities{Networks: true}))
	assertContains(t, html, `action="/networks"`)
	assertContains(t, html, `value="bridge"`)
	assertContains(t, html, `name="name"`)
}

func TestErrorToastRendersMessage(t *testing.T) {
	html := render(t, ErrorToast("boom"))
	assertContains(t, html, "boom")
	assertContains(t, html, "data-tui-toast")
	assertContains(t, html, `role="alert"`) // announced to screen readers
	assertContains(t, html, `aria-live="assertive"`)
}

func TestProfilesPageListsProfiles(t *testing.T) {
	caps := backend.Capabilities{Profiles: true}
	html := render(t, ProfilesPage(caps, []backend.Profile{
		{Name: "default", Description: "d"},
		{Name: "gpu", Description: "g", UsedBy: []string{"demo"}},
	}))
	assertContains(t, html, "default")
	assertContains(t, html, "/profiles/gpu")
}

func TestProfileDetailPageRendersConfigAndDevices(t *testing.T) {
	caps := backend.Capabilities{Profiles: true}
	html := render(t, ProfileDetailPage(caps, backend.Profile{
		Name:    "gpu",
		Config:  map[string]string{"limits.cpu": "2"},
		Devices: map[string]map[string]string{"gpu0": {"type": "gpu"}},
	}))
	assertContains(t, html, "gpu0")
	assertContains(t, html, "limits.cpu")
}

func TestInstanceProfilesFormReflectsAssigned(t *testing.T) {
	inst := backend.Instance{Name: "demo", Profiles: []string{"default"}}
	all := []backend.Profile{{Name: "default"}, {Name: "gpu"}}
	html := render(t, InstanceProfilesForm(inst, all))
	assertContains(t, html, "/instances/demo/profiles")
	assertContains(t, html, `value="default"`)
	assertContains(t, html, `value="gpu"`)
}

func TestDevicesSectionLocalHasRemoveInheritedDoesNot(t *testing.T) {
	caps := backend.Capabilities{Config: true, Devices: true}
	html := render(t, DevicesSection(caps, "demo", backend.InstanceConfig{
		Devices: map[string]map[string]string{
			"root": {"type": "disk", "path": "/"},          // inherited
			"web":  {"type": "proxy", "listen": "tcp::80"}, // local
		},
		LocalDevices: map[string]map[string]string{
			"web": {"type": "proxy", "listen": "tcp::80"},
		},
	}))
	assertContains(t, html, `hx-post="/instances/demo/devices/web/delete"`) // local removable
	if strings.Contains(html, `/instances/demo/devices/root/delete`) {
		t.Fatalf("inherited device must not have a Remove button: %q", html)
	}
	assertContains(t, html, `name="type" value="proxy"`) // an add form
	assertContains(t, html, `name="type" value="disk"`)
}

func TestDevicesSectionEscapesDeviceNameInRemoveURL(t *testing.T) {
	html := render(t, DevicesSection(backend.Capabilities{Devices: true}, "demo", backend.InstanceConfig{
		Devices:      map[string]map[string]string{"a/b": {"type": "disk"}},
		LocalDevices: map[string]map[string]string{"a/b": {"type": "disk"}},
	}))
	assertContains(t, html, "/instances/demo/devices/a%2Fb/delete")
}

func TestDevicesSectionGatesEditingOnCapability(t *testing.T) {
	cfg := backend.InstanceConfig{
		Devices:      map[string]map[string]string{"web": {"type": "proxy"}},
		LocalDevices: map[string]map[string]string{"web": {"type": "proxy"}},
	}
	off := render(t, DevicesSection(backend.Capabilities{Config: true}, "demo", cfg))
	if strings.Contains(off, "/devices/web/delete") {
		t.Fatalf("Remove must be hidden without caps.Devices: %q", off)
	}
	if strings.Contains(off, `name="type"`) {
		t.Fatalf("add forms must be hidden without caps.Devices: %q", off)
	}
}

func TestConfigPanelRendersRows(t *testing.T) {
	html := render(t, ConfigPanel("demo", backend.InstanceConfig{
		Config: map[string]string{"security.nesting": "true"},
	}))
	assertContains(t, html, `hx-post="/instances/demo/config"`)
	assertContains(t, html, `value="security.nesting"`)
	assertContains(t, html, `value="true"`)
	assertContains(t, html, `name="key"`)
}

func TestInstanceBodyGatesConfigAndDevicesTabs(t *testing.T) {
	on := render(t, InstanceBody(backend.Capabilities{Config: true},
		backend.Instance{Name: "demo", Status: "Running"}, nil, nil, "devices"))
	assertContains(t, on, `hx-get="/instances/demo?tab=config"`)  // Configuration tab link
	assertContains(t, on, `hx-get="/instances/demo?tab=devices"`) // Devices tab link
	assertContains(t, on, `hx-get="/instances/demo/devices"`)     // active Devices panel mount

	off := render(t, InstanceBody(backend.Capabilities{},
		backend.Instance{Name: "demo", Status: "Running"}, nil, nil, "devices"))
	if strings.Contains(off, `tab=devices`) || strings.Contains(off, `tab=config`) {
		t.Fatalf("config/devices tabs must be hidden without the capability, got %q", off)
	}
	assertContains(t, off, "Details") // falls back to summary
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
	closingText := ">" + label + "<"
	idx := strings.Index(html, closingText)
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
