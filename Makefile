.PHONY: build dev test test-integration generate vet clean

build:
	./scripts/build.sh

dev:
	./scripts/dev.sh

generate:
	templ generate

test:
	go test ./...

# Requires a reachable Incus daemon; uses the current incus remote.
test-integration:
	go test -tags integration ./internal/backend/incus -v

vet:
	go vet ./...

clean:
	rm -f lxcon
	rm -rf dist
	rm -f static/css/app.css
