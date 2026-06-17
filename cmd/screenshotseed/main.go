// Command screenshotseed serves the Lexi UI backed by a richly-populated fake
// backend. It's a developer tool for capturing README/marketing screenshots
// without a real Incus daemon — a fleet of instances with mixed states,
// resource limits, snapshots, and live metrics. It is not part of the release
// build (scripts build ./cmd/lexi only).
//
// Usage:
//
//	go run ./cmd/screenshotseed            # serves http://127.0.0.1:8099
//	go run ./cmd/screenshotseed --addr :9000
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/lexihq/lexi/internal/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8099", "address to listen on")
	flag.Parse()

	ctx := context.Background()
	b := fake.New()

	type seed struct {
		name     string
		image    string
		cpu, mem string
		profiles []string
		running  bool
		snaps    int
	}
	fleet := []seed{
		{"web-01", "ubuntu/24.04", "2", "2GiB", []string{"default"}, true, 2},
		{"web-02", "ubuntu/24.04", "2", "2GiB", []string{"default"}, true, 0},
		{"api-gateway", "debian/12", "4", "4GiB", []string{"default"}, true, 1},
		{"postgres-01", "debian/12", "4", "8GiB", []string{"default"}, true, 3},
		{"redis-cache", "alpine/edge", "1", "512MiB", []string{"default"}, true, 0},
		{"build-runner", "ubuntu/24.04", "8", "16GiB", []string{"default", "gpu"}, true, 0},
		{"staging-api", "debian/12", "2", "4GiB", []string{"default"}, true, 0},
		{"worker-01", "ubuntu/22.04", "4", "4GiB", []string{"default"}, false, 1},
		{"legacy-box", "debian/11", "1", "1GiB", []string{"default"}, false, 0},
	}

	must := func(err error) {
		if err != nil {
			slog.Error("seed", "err", err)
			os.Exit(1)
		}
	}

	for _, s := range fleet {
		must(b.CreateInstance(ctx, backend.CreateOptions{
			Name:     s.name,
			Image:    s.image,
			Start:    s.running,
			Profiles: s.profiles,
			Config:   map[string]string{"limits.cpu": s.cpu, "limits.memory": s.mem},
		}))
		for i := range s.snaps {
			snap := []string{"daily-backup", "pre-upgrade", "checkpoint"}[i%3]
			must(b.CreateSnapshot(ctx, s.name, snap, backend.SnapshotOptions{}))
		}
	}

	// One frozen instance for status variety.
	must(b.PauseInstance(ctx, "staging-api"))

	// A VM image so the Images page shows more than the seeded container image.
	b.SeedSplitImage("fake-vm-noble-aarch64", "Ubuntu 24.04 LTS VM image")
	// A cancelable running task for the Tasks panel.
	b.SeedRunningOperation(`Migrating instance "postgres-01"`)

	srv := server.New(b, server.WithMetricsSampler(ctx))
	srv.Addr = *addr
	slog.Info("screenshotseed listening", "addr", *addr)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}
