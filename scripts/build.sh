#!/usr/bin/env bash
# Build the single self-contained lxcon binary:
#   templ generate -> tailwind build -> go build (CGO disabled, assets embedded).
set -euo pipefail
cd "$(dirname "$0")/.."

export PATH="$(go env GOPATH)/bin:$PATH"

# Locate a Tailwind CLI: prefer the vendored standalone binary, then one on
# PATH, else fall back to the node package (npx) since node is a dev dep.
if [ -x ./bin/tailwindcss ]; then
	TAILWIND=(./bin/tailwindcss)
elif command -v tailwindcss >/dev/null 2>&1; then
	TAILWIND=(tailwindcss)
else
	TAILWIND=(npx --yes @tailwindcss/cli)
fi

echo "==> templ generate"
templ generate

echo "==> tailwind build"
"${TAILWIND[@]}" -i internal/ui/input.css -o static/css/app.css --minify

echo "==> go build"
CGO_ENABLED=0 go build -o lxcon ./cmd/lxcon

echo "built ./lxcon"
