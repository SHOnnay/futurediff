# FutureDiff Tasks 091–095 validation

## Repository validation

| Check | Result |
|---|---|
| `gofmt` | PASS |
| `go vet ./...` | PASS |
| `go test ./...` | PASS |
| `go test -race ./...` | PASS |
| `go test -cover ./...` | PASS |
| Go commands built | 70 PASS |
| JSON examples/schemas parsed | 60 PASS |
| OpenAPI/API contract consistency | PASS |
| One-command transaction demo | COMMITTED |
| Demo live checkout unchanged | PASS |

## Tenant-isolation live process validation

The test started a real daemon with kernel peer authentication for UIDs `0` and `65534`.

| Control | Result |
|---|---|
| Different UID owners persisted | PASS |
| Owner-scoped list count | 1 |
| Cross-tenant unshared read | REJECTED |
| Read share allows retrieval | PASS |
| Read share allows mutation | REJECTED |
| Operate share allows sealing | PASS |
| Shared principal access administration | REJECTED |
| Revoked read | REJECTED |
| Access event chain | VALID |
| Tenant conformance checks | 13 PASS / 0 FAIL |

## Release validation

| Check | Result |
|---|---|
| Release version | v0.95.0 |
| Release binaries | 70 |
| Offline release checks | 75 PASS / 0 FAIL / 1 SKIP |
| SPDX 2.3 SBOM | PASS |
| in-toto/SLSA provenance | PASS |
| Hosted GitHub attestation | SKIPPED — local release |

## Claims boundary

The live test validates local Linux UID isolation over a Unix socket. It does not certify enterprise IAM, remote transport security, external providers, containers, agent runtimes or hosted signing.
