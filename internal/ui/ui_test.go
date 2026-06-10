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

func TestSidebarGatesStorageLink(t *testing.T) {
	with := render(t, Sidebar(backend.Capabilities{Storage: true}, Nav{Section: NavStorage}))
	assertContains(t, with, "/storage")
	assertContains(t, with, "Storage")

	without := render(t, Sidebar(backend.Capabilities{}, Nav{Section: NavInstances}))
	if strings.Contains(without, `href="/storage"`) {
		t.Fatalf("storage link must be hidden without the capability, got %q", without)
	}
}

func TestSidebarGatesServerLink(t *testing.T) {
	with := render(t, Sidebar(backend.Capabilities{ServerAdmin: true}, Nav{Section: NavServer}))
	assertContains(t, with, "/server")
	assertContains(t, with, "Server")

	without := render(t, Sidebar(backend.Capabilities{}, Nav{Section: NavInstances}))
	if strings.Contains(without, `href="/server"`) {
		t.Fatalf("server link must be hidden without the capability, got %q", without)
	}
}

func TestServerPageRendersSections(t *testing.T) {
	html := render(t, ServerPage(testCaps(),
		backend.ServerOverview{ServerVersion: "6.23", Kernel: "Linux", KernelVersion: "6.8", Driver: "lxc", DriverVersion: "6.0", CPUThreads: 16, MemoryUsed: 1 << 30, MemoryTotal: 8 << 30},
		map[string]string{"core.https_address": ":8443"}, "etag-1",
		[]backend.Certificate{{Name: "laptop", Type: "client", Fingerprint: "abcdef0123456789", Restricted: true}},
		[]backend.Warning{{UUID: "w-1", Severity: "high", Status: "new", Count: 2, LastMessage: "boom"}}))

	assertContains(t, html, "6.23")
	assertContains(t, html, `value="core.https_address"`)
	assertContains(t, html, `name="version" value="etag-1"`)
	assertContains(t, html, `action="/server/config"`)
	assertContains(t, html, "laptop")
	assertContains(t, html, "restricted")
	assertContains(t, html, "boom")
	assertContains(t, html, `hx-post="/server/warnings/w-1/delete"`)
}

func TestWarningsTableEmptyState(t *testing.T) {
	html := render(t, WarningsTable(nil))
	assertContains(t, html, "No warnings")
}

func TestFilesPanelRendersEntriesAndControls(t *testing.T) {
	html := render(t, FilesPanel("demo", "/etc", []backend.FileEntry{
		{Name: "ssl", Dir: true, Mode: "0755"},
		{Name: "hostname", Mode: "0644"},
	}))

	// Breadcrumb + parent navigation re-target the panel.
	assertContains(t, html, `hx-get="/instances/demo/files?path=%2F"`)
	// Directory descent and file download.
	assertContains(t, html, `hx-get="/instances/demo/files?path=%2Fetc%2Fssl"`)
	assertContains(t, html, `href="/instances/demo/files/download?path=%2Fetc%2Fhostname"`)
	assertContains(t, html, `hx-boost="false"`)
	// Upload form posts multipart to the current directory.
	assertContains(t, html, `hx-post="/instances/demo/files/upload"`)
	assertContains(t, html, `hx-encoding="multipart/form-data"`)
	assertContains(t, html, `value="/etc"`)
	assertContains(t, html, "0644")
}

func TestInstanceBodyGatesFilesTab(t *testing.T) {
	caps := testCaps()
	caps.Files = true
	with := render(t, InstanceBody(caps, backend.Instance{Name: "demo"}, nil, nil, "files"))
	assertContains(t, with, `hx-get="/instances/demo/files"`)

	// Without the capability the tab downgrades to summary and never mounts.
	without := render(t, InstanceBody(testCaps(), backend.Instance{Name: "demo"}, nil, nil, "files"))
	if strings.Contains(without, "/instances/demo/files") {
		t.Fatalf("files tab must be hidden without the capability, got %q", without)
	}
}

func TestLayoutGatesOperationsPanel(t *testing.T) {
	caps := testCaps()
	caps.Operations = true
	with := render(t, InstancesPage(caps, nil))
	assertContains(t, with, `hx-get="/partials/operations"`)
	assertContains(t, with, "Tasks")

	without := render(t, InstancesPage(testCaps(), nil))
	if strings.Contains(without, "/partials/operations") {
		t.Fatalf("operations panel must be hidden without the capability, got %q", without)
	}
}

func TestOperationRowsRenderStatusAndEmptyState(t *testing.T) {
	html := render(t, OperationRows([]backend.Operation{
		{Description: `Starting instance "demo"`, Class: "task", Status: "Success"},
		{Description: `Stopping instance "demo"`, Class: "task", Status: "Failure", Err: "boom"},
	}))
	assertContains(t, html, "Starting instance")
	assertContains(t, html, "Success")
	assertContains(t, html, `title="boom"`)

	empty := render(t, OperationRows(nil))
	assertContains(t, empty, "No recent tasks")
}

func TestSidebarGatesImagesLink(t *testing.T) {
	with := render(t, Sidebar(backend.Capabilities{ImageManagement: true}, Nav{Section: NavImages}))
	assertContains(t, with, "/images")
	assertContains(t, with, "Images")

	without := render(t, Sidebar(backend.Capabilities{}, Nav{Section: NavInstances}))
	if strings.Contains(without, `href="/images"`) {
		t.Fatalf("images link must be hidden without the capability, got %q", without)
	}
}

func TestImagesPageRendersFormsAndTable(t *testing.T) {
	html := render(t, ImagesPage(testCaps(),
		[]backend.LocalImage{{Fingerprint: "abcdef0123456789", Aliases: []string{"debian/12"}, Description: "Debian", Arch: "aarch64", SizeBytes: 300 * 1024 * 1024, Type: "container"}},
		[]backend.Instance{{Name: "demo", Status: "Stopped"}}))

	assertContains(t, html, `hx-post="/images/copy"`)
	assertContains(t, html, `hx-post="/images/publish"`)
	assertContains(t, html, `<option value="demo">`)
	assertContains(t, html, "debian/12")
	assertContains(t, html, "abcdef012345") // truncated fingerprint
	assertContains(t, html, "300.0 MiB")
	assertContains(t, html, `hx-post="/images/abcdef0123456789/delete"`)
	assertContains(t, html, `hx-post="/images/abcdef0123456789/aliases"`)
	assertContains(t, html, `hx-post="/images/aliases/delete"`)
}

func TestImagesTableEmptyState(t *testing.T) {
	html := render(t, ImagesTable(nil))
	assertContains(t, html, "No local images yet")
}

func TestStorageVolumesTableShowsDeleteAndCreateForm(t *testing.T) {
	html := render(t, StorageVolumesTable("default", []backend.StorageVolume{
		{Name: "vol1", ContentType: "filesystem"},
	}))
	assertContains(t, html, `action="/storage/default/volumes"`)
	assertContains(t, html, `value="filesystem"`)
	assertContains(t, html, `value="block"`)
	assertContains(t, html, `hx-post="/storage/default/volumes/vol1/delete"`)
}

func TestStorageURLsEscapeSpecialNames(t *testing.T) {
	// Incus permits names like "a#b"; path segments must be escaped so the link
	// targets the whole name (not just "a", with "#b" read as a URL fragment).
	vols := render(t, StorageVolumesTable("pool#1", []backend.StorageVolume{
		{Name: "a#b", ContentType: "filesystem"},
	}))
	assertContains(t, vols, "/storage/pool%231/volumes/a%23b")
	assertContains(t, vols, `hx-post="/storage/pool%231/volumes/a%23b/delete"`)

	snaps := render(t, StorageVolumeSnapshotsTable("pool#1", "a#b", []backend.StorageVolumeSnapshot{
		{Name: "s#0"},
	}))
	assertContains(t, snaps, `hx-post="/storage/pool%231/volumes/a%23b/snapshots/s%230/restore"`)
	assertContains(t, snaps, `hx-post="/storage/pool%231/volumes/a%23b/snapshots/s%230/delete"`)
}

func TestStorageVolumeSnapshotsTableHasCreateAndActions(t *testing.T) {
	html := render(t, StorageVolumeSnapshotsTable("default", "vol1", []backend.StorageVolumeSnapshot{
		{Name: "snap0"},
	}))
	assertContains(t, html, `id="volume-snapshots"`)
	assertContains(t, html, `action="/storage/default/volumes/vol1/snapshots"`)
	assertContains(t, html, `hx-post="/storage/default/volumes/vol1/snapshots/snap0/restore"`)
	assertContains(t, html, `hx-post="/storage/default/volumes/vol1/snapshots/snap0/delete"`)
}

func TestSnapshotTableShowsStatefulCheckboxAndExpiry(t *testing.T) {
	html := render(t, SnapshotTable("demo", nil))
	assertContains(t, html, `name="stateful"`)
	assertContains(t, html, `type="datetime-local"`)
	assertContains(t, html, `name="expires_at"`)
}

func TestSnapshotTableShowsStatefulBadgeAndRowActions(t *testing.T) {
	html := render(t, SnapshotTable("demo", []backend.Snapshot{{Name: "snap0", Stateful: true}}))
	assertContains(t, html, ">stateful<")
	assertContains(t, html, `hx-post="/instances/demo/snapshots/snap0/rename"`)
	assertContains(t, html, `hx-post="/instances/demo/snapshots/snap0/expiry"`)
	assertContains(t, html, `name="new_name"`)
}

func TestSnapshotScheduleFormPrefilled(t *testing.T) {
	html := render(t, SnapshotScheduleForm("demo", backend.SnapshotSchedule{Schedule: "@daily", Expiry: "2w", Pattern: "snap%d"}))
	assertContains(t, html, `id="snapshot-schedule"`)
	assertContains(t, html, `hx-post="/instances/demo/snapshots/schedule"`)
	assertContains(t, html, `value="@daily"`)
	assertContains(t, html, `value="2w"`)
	assertContains(t, html, `value="snap%d"`)
}

func TestInstanceRowShowsMoveControlsWhenCapable(t *testing.T) {
	html := render(t, InstanceRow(backend.Capabilities{Move: true}, backend.Instance{Name: "demo", Status: "Stopped"}))
	assertContains(t, html, `action="/instances/demo/rename"`)
	assertContains(t, html, `action="/instances/demo/move"`)
	assertContains(t, html, `name="new_name"`)
	assertContains(t, html, `name="pool"`)

	off := render(t, InstanceRow(backend.Capabilities{}, backend.Instance{Name: "demo", Status: "Stopped"}))
	if strings.Contains(off, "/instances/demo/rename") {
		t.Fatalf("move controls must be hidden without caps.Move: %q", off)
	}
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
	assertContains(t, html, `>true</textarea>`) // values render as textarea text
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
