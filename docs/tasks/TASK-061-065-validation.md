# FutureDiff Tasks 061–065 Validation

## Summary

All locally executable checks passed.

| Check | Result |
|---|---|
| `gofmt` | PASS |
| `go vet ./...` | PASS |
| `go test ./...` | PASS |
| `go test -race ./...` | PASS |
| `go test -cover ./...` | PASS |
| Go commands built | 48 |
| Demo transaction | COMMITTED |
| Live checkout protection | PASS |
| Signed operator receipt chain | PASS — 2 receipts |
| Receipt tampering | REJECTED |
| Effect graph JSON | PASS |
| Effect graph Mermaid | PASS |
| Effect graph DOT | PASS |
| Local SLO positive gate | PASS |
| Daemon-required SLO negative gate | REJECTED |
| Retention policy dry-run | PASS |
| Apply-disabled retention attempt | REJECTED |
| Readiness positive gate | PASS |
| API-contract mismatch readiness gate | REJECTED |
| JSON schemas and examples | PASS |
| v0.65.0 offline release verification | 53 PASS / 0 FAIL / 1 SKIP |
| Source ZIP integrity | PASS |
| Full bundle ZIP integrity | PASS |

## External limitations

No real Docker/Podman rootless host, GitHub test repository, Slack test channel, OpenCode/Hermes installation, hosted GitHub release, or macOS runner was available. No external certification is claimed.
