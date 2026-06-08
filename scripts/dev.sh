#!/usr/bin/env bash
# Live-reload dev loop: Tailwind --watch alongside templ --watch, which proxies
# localhost:8080 and rebuilds/reruns the server on change.
set -euo pipefail
cd "$(dirname "$0")/.."

GOPATH_BIN="$(go env GOPATH)/bin"
export PATH="$GOPATH_BIN:$PATH"

# Keep templ's watch-mode string files out of the OS temp directory. macOS may
# remove those files while the generated server is still reading them.
mkdir -p tmp
TEMPL_DEV_MODE_ROOT="$(mktemp -d "$PWD/tmp/templ-dev.XXXXXX")"
export TEMPL_DEV_MODE_ROOT

if [ -x ./bin/tailwindcss ]; then
	TAILWIND=(./bin/tailwindcss)
elif command -v tailwindcss >/dev/null 2>&1; then
	TAILWIND=(tailwindcss)
else
	TAILWIND=(npx --yes @tailwindcss/cli)
fi

"${TAILWIND[@]}" -i internal/ui/input.css -o static/css/app.css --watch &
TAILWIND_PID=$!
cleanup() {
	kill "$TAILWIND_PID" 2>/dev/null || true
	rm -rf "$TEMPL_DEV_MODE_ROOT"
}
trap cleanup EXIT

templ generate --watch --proxy="http://localhost:8080" --cmd="go run ./cmd/lxcon"
