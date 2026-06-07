// Command lxcon serves the Lexicon web UI for managing Incus LXC containers.
package main

import (
	"log"
	"net/http"

	"github.com/adam/lxcon/internal/ui"
	"github.com/adam/lxcon/static"

	"github.com/a-h/templ"
)

func main() {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static.FS)))
	mux.Handle("GET /", templ.Handler(ui.InstancesPage()))

	const addr = ":8080"
	log.Printf("lxcon listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
