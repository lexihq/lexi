.PHONY: build release dev test test-integration test-e2e e2e-setup generate lint vet clean

build:
	./scripts/build.sh

release:
	./scripts/release.sh

dev:
	./scripts/dev.sh

generate:
	templ generate
	./scripts/tailwind.sh

test: generate
	go test ./...

lint: generate
	golangci-lint run ./...

# Requires a reachable Incus daemon; uses the current incus remote.
test-integration:
	go test -tags integration ./internal/backend/incus -v

# One-time setup for the browser e2e: install node deps and the Chromium build.
e2e-setup:
	cd e2e && npm install && npx playwright install chromium

# Browser end-to-end test (Playwright). Needs `make e2e-setup` once first. The
# Playwright webServer boots a fake-backed server, so no Incus daemon is needed;
# generate ensures the templ output the fakeserver imports is up to date.
test-e2e: generate
	cd e2e && npx playwright test

vet: generate
	go vet ./...

clean:
	rm -f lexi
	rm -rf dist
	rm -f static/css/app.css
