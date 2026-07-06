package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/lexihq/lexi/internal/ui"
)

// TestBulkOpsMatchRegistry guards the bulk-action registry (ui.BulkActions, the
// source of truth for the bar) and the server's behavior map against drift: an
// action with a button but no backend op would 400 at runtime, and an op with no
// button would be dead code.
func TestBulkOpsMatchRegistry(t *testing.T) {
	if len(bulkOps) != len(ui.BulkActions) {
		t.Fatalf("bulkOps has %d entries, ui.BulkActions has %d", len(bulkOps), len(ui.BulkActions))
	}
	for _, a := range ui.BulkActions {
		if bulkOps[a.Key] == nil {
			t.Errorf("ui.BulkActions has %q but bulkOps has no behavior for it", a.Key)
		}
	}
}

// seedInstances creates the named stopped instances in a fresh fake backend.
func seedInstances(t *testing.T, names ...string) backend.Backend {
	t.Helper()
	b := fake.New()
	for _, n := range names {
		if err := b.CreateInstance(t.Context(), backend.CreateOptions{Name: n, Image: "debian/12"}); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

func TestBulkStartOnlyActsOnSelected(t *testing.T) {
	b := seedInstances(t, "web-1", "web-2", "web-3")
	form := url.Values{"action": {"start"}, "name": {"web-1", "web-3"}}

	res := formRequest(t, New(b), "/instances/bulk", form, true)

	assertStatus(t, res, http.StatusOK)
	if body := res.Body.String(); !strings.Contains(body, "data-tui-toast") {
		t.Fatalf("expected a summary toast, got %q", body)
	}
	for n, want := range map[string]backend.InstanceStatus{"web-1": "Running", "web-2": "Stopped", "web-3": "Running"} {
		inst, err := b.GetInstance(t.Context(), n)
		if err != nil {
			t.Fatal(err)
		}
		if inst.Status != want {
			t.Fatalf("%s: expected %s, got %s", n, want, inst.Status)
		}
	}
}

func TestBulkSnapshotSnapshotsOnlySelected(t *testing.T) {
	b := seedInstances(t, "a", "b", "c")
	form := url.Values{"action": {"snapshot"}, "name": {"a", "c"}}

	res := formRequest(t, New(b), "/instances/bulk", form, true)

	assertStatus(t, res, http.StatusOK)
	for n, want := range map[string]int{"a": 1, "b": 0, "c": 1} {
		snaps, err := b.ListSnapshots(t.Context(), n)
		if err != nil {
			t.Fatal(err)
		}
		if len(snaps) != want {
			t.Fatalf("%s: expected %d snapshot(s), got %d", n, want, len(snaps))
		}
	}
}

func TestBulkDeleteRemovesSelected(t *testing.T) {
	b := seedInstances(t, "a", "b")
	form := url.Values{"action": {"delete"}, "name": {"a"}}

	res := formRequest(t, New(b), "/instances/bulk", form, true)

	assertStatus(t, res, http.StatusOK)
	if _, err := b.GetInstance(t.Context(), "a"); err == nil {
		t.Fatal("expected a to be deleted")
	}
	if _, err := b.GetInstance(t.Context(), "b"); err != nil {
		t.Fatalf("expected b to remain, got %v", err)
	}
}

func TestBulkNonHTMXRedirects(t *testing.T) {
	b := seedInstances(t, "a")
	form := url.Values{"action": {"start"}, "name": {"a"}}

	res := formRequest(t, New(b), "/instances/bulk", form, false)

	assertStatus(t, res, http.StatusSeeOther)
	if loc := res.Header().Get("Location"); loc != "/" {
		t.Fatalf("expected redirect to /, got %q", loc)
	}
}

func TestInstancesPartialReturnsTableFragment(t *testing.T) {
	b := seedInstances(t, "web-1")

	res := request(t, New(b), "GET", "/partials/instances", "", true)

	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	if !strings.Contains(body, `id="instances-table"`) || !strings.Contains(body, "web-1") {
		t.Fatalf("expected the table fragment with the instance, got %q", body)
	}
	// A fragment for the poll, not the full page shell.
	if strings.Contains(body, "<html") {
		t.Fatalf("expected a fragment, got a full page: %q", body)
	}
}

func TestBulkRejectsEmptySelectionAndUnknownAction(t *testing.T) {
	b := seedInstances(t, "a")
	cases := []url.Values{
		{"action": {"start"}},                  // no names
		{"name": {"a"}},                        // no action
		{"action": {"explode"}, "name": {"a"}}, // unknown action
	}
	for _, form := range cases {
		res := formRequest(t, New(b), "/instances/bulk", form, true)
		assertStatus(t, res, http.StatusBadRequest)
	}
	// The instance is untouched after the rejected requests.
	inst, err := b.GetInstance(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status != "Stopped" {
		t.Fatalf("expected a untouched (Stopped), got %s", inst.Status)
	}
}

// A batch with a failing member must report the failure by name in the toast
// (and still apply the rest) — the partial-failure branch of the concurrent
// apply path.
func TestBulkPartialFailureReportsFailedNames(t *testing.T) {
	b := seedInstances(t, "a", "b")
	form := url.Values{"action": {"start"}, "name": {"a", "ghost", "b"}}

	res := formRequest(t, New(b), "/instances/bulk", form, true)

	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	if !strings.Contains(body, "failed: ghost") {
		t.Fatalf("expected the toast to name the failed instance, got %q", body)
	}
	for _, n := range []string{"a", "b"} {
		inst, err := b.GetInstance(t.Context(), n)
		if err != nil {
			t.Fatal(err)
		}
		if inst.Status != backend.StatusRunning {
			t.Fatalf("%s: expected Running despite the partial failure, got %s", n, inst.Status)
		}
	}
}
