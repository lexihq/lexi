// Command lxcon serves the Lexicon web UI for managing Incus LXC containers.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/adam/lxcon/internal/backend/incus"
	"github.com/adam/lxcon/internal/server"
)

const defaultListenAddr = "127.0.0.1:8080"

func main() {
	addr := flag.String("addr", defaultListenAddr, "address to listen on")
	incusRemote := flag.String("incus-remote", "", "Incus CLI remote to use (defaults to current remote)")
	flag.Parse()

	if *incusRemote != "" {
		if err := os.Setenv("LXCON_INCUS_REMOTE", *incusRemote); err != nil {
			slog.Error("set incus remote", "err", err)
			os.Exit(1)
		}
	}

	b, err := incus.New()
	if err != nil {
		slog.Error("initialize incus backend", "err", err)
		os.Exit(1)
	}

	srv := server.New(b, server.WithMetricsSampler(context.Background()))
	srv.Addr = *addr

	slog.Info("listening", "addr", *addr)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("serve", "addr", *addr, "err", err)
		os.Exit(1)
	}
}
