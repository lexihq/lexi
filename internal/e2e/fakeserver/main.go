// Command fakeserver serves the lexi web UI backed by the in-memory fake
// backend, seeded with one instance. It exists only to drive the Playwright
// end-to-end tests without a real Incus daemon, so the test double never ships
// in the production lexi binary.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/lexihq/lexi/internal/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8099", "address to listen on")
	instance := flag.String("instance", "demo", "name of the seeded instance")
	flag.Parse()

	b := fake.New()
	if err := b.CreateInstance(context.Background(), backend.CreateOptions{Name: *instance, Image: "debian/12"}); err != nil {
		slog.Error("seed instance", "err", err)
		os.Exit(1)
	}
	// A binary file so the e2e suite can assert the editor refuses it.
	if err := b.PushFile(context.Background(), *instance, "/root/blob.bin",
		strings.NewReader("\x7fELF\x00\x01\x02"), backend.FileWriteOptions{}); err != nil {
		slog.Error("seed binary file", "err", err)
		os.Exit(1)
	}
	// A log with control bytes the editor refuses but the read-only viewer shows.
	if err := b.PushFile(context.Background(), *instance, "/root/app.log",
		strings.NewReader("boot ok\nstarting service\x00\xff\nlistening\n"), backend.FileWriteOptions{}); err != nil {
		slog.Error("seed log file", "err", err)
		os.Exit(1)
	}
	// A cancelable running task so the e2e suite can exercise the Tasks panel
	// cancel control (the fake's normal log only holds finished operations).
	b.SeedRunningOperation(`Migrating instance "demo"`)
	// A VM (split) image so the e2e suite can exercise the zip export/import
	// round-trip (PublishImage only makes container images).
	b.SeedSplitImage("fake-vm-noble-aarch64", "Ubuntu Noble VM image")
	// Config drift: the default profile sets a key the instance overrides
	// locally, so the Configuration tab shows the "overrides profile" badge.
	if err := b.UpdateProfile(context.Background(), "default", "Default Incus profile",
		map[string]string{"user.tier": "standard"}, ""); err != nil {
		slog.Error("seed profile config", "err", err)
		os.Exit(1)
	}
	if err := b.UpdateInstanceConfig(context.Background(), *instance,
		map[string]string{"user.tier": "gold"}, ""); err != nil {
		slog.Error("seed instance config drift", "err", err)
		os.Exit(1)
	}

	srv := server.New(b, server.WithMetricsSampler(context.Background()))
	srv.Addr = *addr

	slog.Info("fakeserver listening", "addr", *addr, "instance", *instance)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("serve", "addr", *addr, "err", err)
		os.Exit(1)
	}
}
