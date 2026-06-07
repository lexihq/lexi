#!/usr/bin/env bash
# Live-reload dev loop: Tailwind --watch alongside templ --watch, which proxies
# localhost:8080 and rebuilds/reruns the server on change.
set -euo pipefail
cd "$(dirname "$0")/.."

GOPATH_BIN="$(go env GOPATH)/bin"
export PATH="$GOPATH_BIN:$PATH"

if [ -x ./bin/tailwindcss ]; then
	TAILWIND=(./bin/tailwindcss)
elif command -v tailwindcss >/dev/null 2>&1; then
	TAILWIND=(tailwindcss)
else
	TAILWIND=(npx --yes @tailwindcss/cli)
fi

"${TAILWIND[@]}" -i internal/ui/input.css -o static/css/app.css --watch &
TAILWIND_PID=$!
trap 'kill "$TAILWIND_PID" 2>/dev/null || true' EXIT

templ generate --watch --proxy="http://localhost:8080" --cmd="go run ./cmd/lxcon"
