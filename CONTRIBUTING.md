# Contributing to Lexi

Thanks for your interest in improving Lexi! This guide covers how to build,
test, and submit changes.

## Prerequisites

- **Go 1.26+**
- **Incus** reachable via the `incus` CLI (only for `integration` tests; unit
  and e2e tests use an in-memory fake and need no daemon)
- [`templ`](https://templ.guide): `go install github.com/a-h/templ/cmd/templ@latest`
- Node.js (for the Tailwind CLI and the Playwright e2e suite)

## Build & test

```bash
make build            # templ generate -> tailwind -> go build (single binary)
make test             # unit tests (fake backend, no daemon)
make lint             # golangci-lint (after templ generate)
make vet              # go vet
make e2e-setup        # one-time: install e2e deps + Chromium
make test-e2e         # Playwright browser tests against the fake-backed server
```

After editing any `.templ` file, run `make generate` before `go test`/`go build`
or you'll be testing stale generated code.

Run `make lint` and `make vet` before opening a pull request.

## Conventions

- **Errors are values** — handle them explicitly; never discard an `error`.
- **Fail fast** — validate inputs at the top of a function.
- The `internal/backend.Backend` interface is the single seam between HTTP and
  the container driver. Don't leak `incus/shared/api` types past the driver.
  Tier-specific features are gated by `Capabilities`, never hardcoded.
- **Tests are first-class.** New behavior follows TDD: write the fake-backed
  test first, then implement against the interface. **Every user-facing flow
  must have matching Playwright e2e coverage** — add or update the relevant test
  in `e2e/tests/` in the same change, and make sure `make test-e2e` passes.

## Commits & pull requests

- Use clear, conventional-style commit messages that describe the change
  (`feat(ui): ...`, `fix(metrics): ...`, `docs: ...`).
- Keep changes focused; every changed line should trace to the stated purpose.
- Ensure `make lint`, `make vet`, `make test`, and `make test-e2e` pass before
  requesting review.

## Contribution terms

Lexi is licensed under the
[PolyForm Noncommercial License 1.0.0](LICENSE), and the maintainer also offers
commercial licenses. To keep that possible, by submitting a contribution you:

1. certify the [Developer Certificate of Origin](https://developercertificate.org/)
   by signing off your commits (`git commit -s`), and
2. agree that your contribution is provided under the project's license **and**
   that the maintainer may also license your contribution (and the project as a
   whole) under other terms, including commercial ones.

If you can't agree to these terms, please open an issue to discuss before
submitting code.
