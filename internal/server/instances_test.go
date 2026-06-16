package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartHXReturnsUpdatedRow(t *testing.T) {
	b := fake.New()
	if err := b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}); err != nil {
		t.Fatal(err)
	}

	res := request(t, New(b), "POST", "/instances/demo/start", "", true)

	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	if !strings.Contains(body, "demo") || !strings.Contains(body, "Running") {
		t.Fatalf("expected updated row with running demo, got %q", body)
	}
	inst, err := b.GetInstance(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status != "Running" {
		t.Fatalf("expected backend status Running, got %q", inst.Status)
	}
}

func TestRestartPauseResumeReturnRowOnHTMX(t *testing.T) {
	cases := []struct {
		path   string
		status string // status reflected in the returned row
	}{
		{"/instances/demo/restart", "Running"},
		{"/instances/demo/pause", "Frozen"},
		{"/instances/demo/resume", "Running"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			b := fake.New()
			if err := b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Start: true}); err != nil {
				t.Fatal(err)
			}

			res := request(t, New(b), "POST", tc.path, "", true)

			assertStatus(t, res, http.StatusOK)
			body := res.Body.String()
			assert.Contains(t, body, `id="instance-demo"`)
			assert.Contains(t, body, tc.status)
		})
	}
}

func TestLifecycleActionUnknownInstanceIs404(t *testing.T) {
	b := fake.New()
	res := request(t, New(b), "POST", "/instances/ghost/restart", "", true)
	assertStatus(t, res, http.StatusNotFound)
}

func TestDeleteHXRemovesRow(t *testing.T) {
	b := fake.New()
	if err := b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}); err != nil {
		t.Fatal(err)
	}

	res := request(t, New(b), "POST", "/instances/demo/delete", "", true)

	assertStatus(t, res, http.StatusOK)
	if body := strings.TrimSpace(res.Body.String()); body != "" {
		t.Fatalf("expected empty htmx delete body, got %q", body)
	}
	instances, err := b.ListInstances(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Fatalf("expected deleted instance, got %#v", instances)
	}
}

func TestHXRequestTogglesPartialVsRedirect(t *testing.T) {
	b := fake.New()
	if err := b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}); err != nil {
		t.Fatal(err)
	}

	hx := request(t, New(b), "POST", "/instances/demo/start", "", true)
	assertStatus(t, hx, http.StatusOK)
	if body := hx.Body.String(); strings.Contains(strings.ToLower(body), "<!doctype") || !strings.Contains(body, "Running") {
		t.Fatalf("expected htmx partial row, got %q", body)
	}

	full := request(t, New(b), "POST", "/instances/demo/stop", "", false)
	assertStatus(t, full, http.StatusSeeOther)
	if loc := full.Header().Get("Location"); loc != "/" {
		t.Fatalf("expected redirect to /, got %q", loc)
	}
}

func TestSidebarPartialListsInstancesWithActive(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), "GET", "/partials/sidebar?active=demo", "", true)

	assert.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	assert.Contains(t, body, "demo")
	assert.Contains(t, body, "bg-accent")   // active=demo highlight threaded through
	assert.Contains(t, body, "bg-gray-400") // stopped status dot
	assert.Contains(t, body, "Stopped")     // status as screen-reader text, not color alone
}

func TestDetailTabReturnsFragmentForHXAndFullPageOtherwise(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))
	srv := New(b)

	// HTMX request gets just the swappable body (no shell), with the requested
	// tab mounted — proving ?tab is forwarded and the fragment branch is taken.
	hx := request(t, srv, "GET", "/instances/demo?tab=metrics", "", true)
	assert.Equal(t, http.StatusOK, hx.Code)
	hxBody := hx.Body.String()
	assert.NotContains(t, strings.ToLower(hxBody), "<!doctype")
	assert.Contains(t, hxBody, `id="instance-body"`)
	assert.Contains(t, hxBody, `hx-get="/instances/demo/metrics"`)

	// A plain request gets the full shell (doctype + sidebar) for reload/deep-link.
	full := request(t, srv, "GET", "/instances/demo?tab=metrics", "", false)
	assert.Equal(t, http.StatusOK, full.Code)
	fullBody := full.Body.String()
	assert.Contains(t, strings.ToLower(fullBody), "<!doctype")
	assert.Contains(t, fullBody, "/partials/sidebar")
	assert.Contains(t, fullBody, `id="instance-body"`)

	// A boosted navigation (HX-Request + HX-Boosted) must get the full page too,
	// so hx-boost swaps the whole shell — not the bare tab fragment.
	boosted := boostedRequest(t, srv, "/instances/demo?tab=metrics")
	assert.Equal(t, http.StatusOK, boosted.Code)
	boostedBody := boosted.Body.String()
	assert.Contains(t, strings.ToLower(boostedBody), "<!doctype")
	assert.Contains(t, boostedBody, "/partials/sidebar")
	assert.Contains(t, boostedBody, `id="instance-body"`)
}

func TestInstanceDetailHeaderHasLifecycleControls(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	res := request(t, New(b), "GET", "/instances/demo", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, `id="instance-header"`)
	// Stopped instance: Start + Restart in the header, posting back to the header.
	assert.Contains(t, body, `hx-post="/instances/demo/start?from=header"`)
	assert.Contains(t, body, `hx-target="#instance-header"`)
}

func TestLifecycleActionFromHeaderReturnsHeaderFragment(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	res := formRequest(t, New(b), "/instances/demo/start?from=header", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, `id="instance-header"`)
	assert.Contains(t, body, "Running")
	assert.NotContains(t, body, "<tr") // header fragment, not the list row
}

func TestLifecycleActionWithoutHeaderStillReturnsRow(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	res := formRequest(t, New(b), "/instances/demo/start", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), `id="instance-demo"`) // the table row
}
