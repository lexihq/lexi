# Security Policy

## Supported versions

Lexi is pre-1.0 and under active development. Only the latest commit on `main`
(and the most recent tagged release, once releases exist) receives security
fixes.

## Reporting a vulnerability

**Please do not open a public issue for security vulnerabilities.**

Report privately through GitHub's
[private vulnerability reporting](https://github.com/lexihq/lexi/security/advisories/new)
(the **Security → Report a vulnerability** button on the repository). Include:

- a description of the issue and its impact,
- steps to reproduce (a proof of concept if possible),
- affected version / commit.

You can expect an initial acknowledgement within a few days. Once a fix is
ready we'll coordinate disclosure with you.

## Important deployment note

Lexi serves an **unauthenticated control plane**. By default it binds to
`127.0.0.1:8080`. Passing `-addr :8080` (or any non-loopback address) exposes
full container management to anyone who can reach that port, with no built-in
authentication. Only expose Lexi on a trusted, access-controlled network (for
example behind an authenticating reverse proxy or a VPN). Treat a reachable
Lexi instance as equivalent to shell access to the host's containers.
