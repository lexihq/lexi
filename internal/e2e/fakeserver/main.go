// Command fakeserver serves the lxcon web UI backed by the in-memory fake
// backend, seeded with one instance. It exists only to drive the Playwright
// end-to-end tests without a real Incus daemon, so the test double never ships
// in the production lxcon binary.
package main

import (
	"context"
	"flag"
	"log"

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
		log.Fatalf("fakeserver: seed instance: %v", err)
	}

	srv := server.New(b)
	srv.Addr = *addr

	log.Printf("fakeserver listening on %s (instance %q)", *addr, *instance)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("fakeserver: serve on %s: %v", *addr, err)
	}
}
