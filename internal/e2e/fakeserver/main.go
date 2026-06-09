// Command fakeserver serves the lxcon web UI backed by the in-memory fake
// backend, seeded with one instance. It exists only to drive the Playwright
// end-to-end tests without a real Incus daemon, so the test double never ships
// in the production lxcon binary.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/adam/lxcon/internal/server"
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

	srv := server.New(b)
	srv.Addr = *addr

	slog.Info("fakeserver listening", "addr", *addr, "instance", *instance)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("serve", "addr", *addr, "err", err)
		os.Exit(1)
	}
}
