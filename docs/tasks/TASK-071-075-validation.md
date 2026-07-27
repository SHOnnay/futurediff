# Tasks 071–075 validation

## Source validation

- `gofmt`: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- package coverage run: PASS
- Go commands built: 55
- JSON schemas/examples/configurations parsed: 45
- one-command transaction demo: PASS
- live checkout remained unchanged: PASS

## Process validation

- signed rate-policy attestation verified: PASS
- daemon startup with required signed configuration: PASS
- active daemon lock inspection: PASS
- second daemon on the same data root: REJECTED
- read rate sequence: HTTP 200, HTTP 200, HTTP 429
- independent mutation bucket: HTTP 201
- API access hash chain: PASS
- modified API access row: REJECTED
- one-byte configuration drift: REJECTED
- secure data-root audit during daemon operation: PASS
- data-root mode 0755: REJECTED
- daemon lock released after graceful shutdown: PASS

## Release validation

- version: v0.75.0
- Linux x86-64 executables: 55
- release files: 61
- offline release checks passed: 60
- offline release checks failed: 0
- offline release checks skipped: 1
- SPDX 2.3 SBOM: PASS
- in-toto/SLSA provenance: PASS
- hosted GitHub signed attestation: SKIPPED because the release was generated locally
