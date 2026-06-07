// Command lxcon serves the Lexicon web UI for managing Incus LXC containers.
package main

import (
	"flag"
	"log"
	"os"

	"github.com/adam/lxcon/internal/backend/incus"
	"github.com/adam/lxcon/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	incusRemote := flag.String("incus-remote", "", "Incus CLI remote to use (defaults to current remote)")
	flag.Parse()

	if *incusRemote != "" {
		if err := os.Setenv("LXCON_INCUS_REMOTE", *incusRemote); err != nil {
			log.Fatalf("lxcon: set incus remote: %v", err)
		}
	}

	b, err := incus.New()
	if err != nil {
		log.Fatalf("lxcon: initialize incus backend: %v", err)
	}

	srv := server.New(b)
	srv.Addr = *addr

	log.Printf("lxcon listening on http://localhost%s", *addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("lxcon: serve on %s: %v", *addr, err)
	}
}
