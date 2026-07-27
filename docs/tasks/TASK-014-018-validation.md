# Tasks 014–018 combined validation

## Passed

- `gofmt` clean
- `go vet ./...`
- `go test ./...`
- `go test -race ./...`
- Build of all eight commands
- One-command demo with unchanged live checkout
- Deterministic benchmark generation
- SPDX 2.3 SBOM generation
- SQLite integrity check
- SQLite online backup, reopen, and integrity validation
- Embedded migration-artifact verification
- Versioned Linux x86-64 release archive
- Release SHA-256 verification

## Environment qualification

Docker and Podman were not installed. The host-certification command correctly produced `certified=false`, recorded a failed `runtime_ready` check, and exited nonzero. The rootless OCI implementation is therefore not claimed as certified on this host.

## Demonstration result

- Final status: `committed`
- Live checkout: `current reality`
- FutureDiff ref: `approved future`
- Live-checkout safety: `true`

## Benchmark result

The synthetic verification-failure scenario released zero effects in FutureDiff mode. Direct and permission-only modes released one unsafe external effect. The lost-response scenario produced one effect in FutureDiff mode and two in the modeled baselines.

## Ledger result

- Integrity: passed
- Migration count: 8
- Transaction count: 1
- Unresolved transactions: 0
- Consistent backup reopened successfully
