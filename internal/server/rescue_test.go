package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
)

func TestRescueSnapshotsThenFreezes(t *testing.T) {
	b := fake.New()
	if err := b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Start: true}); err != nil {
		t.Fatal(err)
	}

	res := request(t, New(b), "POST", "/instances/demo/rescue", "", true)

	assertStatus(t, res, http.StatusOK)
	if body := res.Body.String(); !strings.Contains(body, "data-tui-toast") {
		t.Fatalf("expected a confirmation toast, got %q", body)
	}

	// Frozen for inspection.
	inst, err := b.GetInstance(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status != "Frozen" {
		t.Fatalf("expected Frozen after rescue, got %s", inst.Status)
	}

	// A rescue-* snapshot checkpoint was created.
	snaps, err := b.ListSnapshots(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range snaps {
		if strings.HasPrefix(s.Name, "rescue-") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a rescue-* snapshot, got %+v", snaps)
	}
}
