# FutureDiff Tasks 096–100 validation

## Repository checks

| Check | Result |
|---|---|
| `gofmt` | PASS |
| `go vet ./...` | PASS |
| `go test ./...` | PASS |
| `go test -race ./...` | PASS |
| `go test -cover ./...` | PASS |
| Go command build | 74 PASS |
| JSON parsing | 87 PASS |
| Makefile/installer/source inventory | 74 / 74 / 74 |
| One-command demo | PASS |
| Live checkout protection | PASS |

## Live daemon checks

| Check | Result |
|---|---|
| Transaction creation | HTTP 201 |
| One-second read grant | HTTP 200 |
| Second active grant under quota 1 | HTTP 409 |
| New grant after first expiry | HTTP 200 |
| Inactive/active access projection | PASS |
| Redacted tenant inventory | PASS |
| Cleanup candidate count | 1 |
| Apply-disabled cleanup | REJECTED |
| Incorrect confirmation | REJECTED |
| Correct cleanup deletion count | 1 |
| Access-event chain verification | PASS |
| Semantic ledger audit | HEALTHY |
| Governance conformance | 15 PASS / 0 FAIL |

## Claims boundary

The validation is local Linux evidence. External container, provider, agent, macOS and hosted-attestation criteria were not available and are not claimed.

## v1.00.0 release verification

| Check | Result |
|---|---|
| Packaged Go binaries | 74 |
| `SHA256SUMS` entries | 76 |
| Offline release checks | 79 PASS / 0 FAIL / 1 SKIP |
| SPDX 2.3 SBOM | PASS |
| SLSA/in-toto provenance | PASS |
| Release archive SHA-256 | PASS |

The single skipped check is hosted GitHub-signed attestation verification because this release was generated locally.

## Package integrity

| Check | Result |
|---|---|
| Complete source ZIP integrity | PASS |
| Full v1.00.0 bundle ZIP integrity | PASS |
| Bundle-internal SHA-256 manifest | PASS |
