# Task 019 — Controlled Provider Egress

## Delivered

- `internal/egress` fail-closed HTTP transport
- Exact HTTPS host, port, method, and path rules
- DNS pinning for the connection attempt
- Private and special-purpose IP rejection
- No redirects and no environment proxy
- TLS 1.2 minimum
- GitHub and Slack daemon integration
- Unit tests for look-alike domains, path confusion, IP literals, loopback, and documentation ranges

## Security boundary

This controls credential-bearing HTTP calls made by the built-in GitHub API and Slack adapters. The create-only Git branch adapter uses Git smart HTTP and remains separately constrained by exact normalized remote URL, disabled redirects, credential-free URL, and scoped broker access.

## Validation

`go test -race ./internal/egress ./internal/adapters/...` passes.
