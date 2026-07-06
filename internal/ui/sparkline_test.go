package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
)

func TestSparkPointsMapsAndClamps(t *testing.T) {
	// 0% maps to the box bottom (y=h), 100% to the top (y=0); oldest is x=0.
	if got := sparkPoints([]float64{0, 100}, 80, 20); got != "0.0,20.0 80.0,0.0" {
		t.Fatalf("got %q", got)
	}
	// Out-of-range samples are clamped into the box.
	if got := sparkPoints([]float64{-50, 150}, 80, 20); got != "0.0,20.0 80.0,0.0" {
		t.Fatalf("clamp: got %q", got)
	}
}

func TestInstanceRowSparklineGatedOnTrendContext(t *testing.T) {
	inst := backend.Instance{Name: "demo", Status: "Running"}

	// With CPU history in context, the row draws a sparkline.
	ctx := WithInstanceTrends(context.Background(), map[string]InstanceTrend{"demo": {CPU: []float64{5, 80, 30}, CPUNow: 30}})
	var withTrend bytes.Buffer
	if err := InstanceRow(testCaps(), inst).Render(ctx, &withTrend); err != nil {
		t.Fatal(err)
	}
	assertContains(t, withTrend.String(), "<polyline")

	// Without it (a single-row swap, a unit test), the row omits the sparkline.
	var noTrend bytes.Buffer
	if err := InstanceRow(testCaps(), inst).Render(context.Background(), &noTrend); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(noTrend.String(), "<polyline") {
		t.Fatalf("expected no sparkline without trend context, got %q", noTrend.String())
	}
}
