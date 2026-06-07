#!/usr/bin/env bash
# Cross-build release binaries for Linux targets.
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

DIST_DIR="${DIST_DIR:-dist}"
TARGETS=("linux/amd64" "linux/arm64")

mkdir -p "$DIST_DIR"

echo "==> templ generate"
templ generate

echo "==> tailwind build"
"${TAILWIND[@]}" -i internal/ui/input.css -o static/css/app.css --minify

for target in "${TARGETS[@]}"; do
	GOOS="${target%/*}"
	GOARCH="${target#*/}"
	OUT="$DIST_DIR/lxcon-$GOOS-$GOARCH"

	echo "==> build $OUT"
	CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build \
		-o "$OUT" ./cmd/lxcon
	chmod +x "$OUT"
done

echo "built release binaries in $DIST_DIR"
