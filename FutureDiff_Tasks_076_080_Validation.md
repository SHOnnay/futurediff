# FutureDiff Tasks 076–080 validation

## Static and automated validation

| Check | Result |
|---|---|
| `gofmt -w .` | PASS |
| `go vet ./...` | PASS |
| `go test ./...` | PASS |
| `go test -race ./...` | PASS |
| `go test -cover ./...` | PASS |
| Go commands built | PASS — 59 |
| JSON examples and schemas parsed | PASS — 48 |

## Executable process validation

| Scenario | Result |
|---|---|
| Repository under allowed canonical root | PASS — HTTP 201 |
| Repository outside allowed root | REJECTED — HTTP 409 |
| Explicit request ID returned in response | PASS |
| Request ID persisted in API audit | PASS |
| API audit chain verification | PASS |
| Signed integrity checkpoint creation | PASS |
| Signed integrity checkpoint verification | PASS |
| Modified ledger-backup verification | REJECTED |
| Expired coordinator lease deletion | PASS — 1 deleted |
| Live coordinator lease preservation | PASS |
| Ledger-maintenance dry run | PASS |
| Pre-maintenance backup | PASS |
| Pre/post semantic ledger audits | PASS |
| SQLite checkpoint/optimize/analyze/vacuum | PASS |
| One-command transaction demo | PASS — committed |
| Live checkout unchanged | PASS |

## Release verification

| Check | Result |
|---|---|
| Release binaries | 59 |
| Offline checks passed | 64 |
| Offline checks failed | 0 |
| Offline checks skipped | 1 |
| SPDX 2.3 SBOM | PASS |
| in-toto/SLSA provenance | PASS |
| Release verified | true |

The single skipped check is hosted GitHub-signed attestation verification because the release was generated locally.

## External claims intentionally excluded

No real Docker, Podman, GitHub, Slack, OpenCode, Hermes, native macOS, performance, or hosted signing certification was performed in this task block.
