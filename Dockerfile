# Build the single self-contained lexi binary and ship it on a minimal base.
# The stylesheet (static/css/app.css) is committed, so the build stage only
# regenerates the templ Go sources; no tailwind/node toolchain is needed.
FROM golang:1.26 AS build
WORKDIR /src

# Cache module downloads separately from the source tree.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Pin templ to the go.mod version; the @version form resolves its own deps
# without needing entries in this module's go.sum.
RUN go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate \
 && CGO_ENABLED=0 GOOS=linux go build -o /lexi ./cmd/lexi

# distroless/static carries CA certificates (needed for TLS Incus remotes and
# images.linuxcontainers.org) and runs as an unprivileged user by default.
FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source="https://github.com/lexihq/lexi"
LABEL org.opencontainers.image.description="Lexicon — web UI for managing Incus LXC containers"
COPY --from=build /lexi /usr/local/bin/lexi
EXPOSE 8080
# Bind all interfaces so a mapped host port reaches the server (the binary
# defaults to 127.0.0.1, which is unreachable from outside the container).
ENTRYPOINT ["lexi", "--addr", "0.0.0.0:8080"]
