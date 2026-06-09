#!/usr/bin/env bash
# Build the single self-contained lxcon binary:
#   templ generate -> tailwind build -> go build (CGO disabled, assets embedded).
set -euo pipefail
cd "$(dirname "$0")/.."

GOPATH_BIN="$(go env GOPATH)/bin"
export PATH="$GOPATH_BIN:$PATH"

echo "==> templ generate"
templ generate

echo "==> tailwind build"
./scripts/tailwind.sh

echo "==> go build"
CGO_ENABLED=0 go build -o lxcon ./cmd/lxcon

echo "built ./lxcon"
