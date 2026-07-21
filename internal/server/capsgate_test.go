package server

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/stretchr/testify/require"
)

// reducedCaps wraps the fake backend with a fixed Capabilities value, so the
// handlers' server-side capability gates — which the all-capable fake can
// never trip — get their false branch exercised.
type reducedCaps struct {
	backend.Backend

	caps backend.Capabilities
}

func (r reducedCaps) Capabilities(context.Context) backend.Capabilities { return r.caps }

// lowered returns a fake-backed backend whose capabilities are the fake's
// defaults with mutate applied, seeded with the named instance.
func lowered(t *testing.T, name string, mutate func(*backend.Capabilities)) backend.Backend {
	t.Helper()
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: name, Image: "debian/12"}))
	caps := b.Capabilities(t.Context())
	mutate(&caps)
	return reducedCaps{Backend: b, caps: caps}
}

func TestBulkActionUnsupportedTierIs422(t *testing.T) {
	// The bulk snapshot button is hidden without the Snapshots capability; a
	// crafted POST must be rejected once up front, not fail per instance.
	b := lowered(t, "demo", func(c *backend.Capabilities) { c.Snapshots = false })
	res := formRequest(t, New(b), "/instances/bulk",
		url.Values{"action": {"snapshot"}, "name": {"demo"}}, true)
	assertStatus(t, res, http.StatusUnprocessableEntity)

	snaps, err := b.ListSnapshots(t.Context(), "demo")
	require.NoError(t, err)
	require.Empty(t, snaps, "the gate must reject before any snapshot is taken")
}

func TestRescueUnsupportedTierIs422(t *testing.T) {
	// Rescue needs Pause && Snapshots; without Pause a crafted POST must not
	// leave a partial mutation (an orphan snapshot with no freeze).
	b := lowered(t, "demo", func(c *backend.Capabilities) { c.Pause = false })
	require.NoError(t, b.StartInstance(t.Context(), "demo"))

	res := request(t, New(b), "POST", "/instances/demo/rescue", "", true)
	assertStatus(t, res, http.StatusUnprocessableEntity)

	snaps, err := b.ListSnapshots(t.Context(), "demo")
	require.NoError(t, err)
	require.Empty(t, snaps, "the gate must reject before the snapshot step")
}
