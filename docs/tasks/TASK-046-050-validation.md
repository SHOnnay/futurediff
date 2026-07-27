# Tasks 046–050 validation

## Automated Go checks

- `gofmt`: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- Go commands built: 35

## Task 046 — signed approvals

- Ed25519 key generation: PASS
- Private-key permission enforcement: PASS
- Envelope signing and independent verification: PASS
- Tamper and expiry rejection: PASS
- Daemon strict signed-approval lifecycle: PASS
- Unsigned approval rejected while strict mode enabled: PASS
- Signed transaction committed: PASS
- Live checkout remained unchanged: PASS

## Task 047 — policy bundles

- Verification contract validation: PASS
- Deterministic archive generation: PASS
- Differently ordered labels produced byte-identical archives: PASS
- Independent bundle verification: PASS

## Task 048 — transaction diff

- JSON summary generation: PASS
- Markdown summary generation: PASS
- Stable path/effect ordering: PASS
- Committed demo transaction summary: PASS

## Task 049 — upgrade rehearsal

- Daemon-offline gate: PASS
- SQLite online copy: PASS
- Migrations applied only to clone: PASS
- Transaction and unresolved counts preserved: PASS
- Source SHA-256 unchanged: PASS
- Post-upgrade audit: PASS

## Task 050 — compatibility harness

- API baseline compatibility: PASS
- Verification contract validation: PASS
- EffectSpec descriptor validation: PASS
- OpenCode profile linting: PASS
- Path traversal rejection: PASS

## Release validation

- v0.50.0 Linux amd64 release built: PASS
- Included commands: 35
- SHA-256 entries: 37
- SPDX 2.3 SBOM: PASS
- in-toto/SLSA provenance: PASS
- Offline checks passed: 40
- Offline checks failed: 0
- Signed GitHub attestation: SKIPPED because the release was built locally

## External limitations

Docker/Podman rootless hosts, live GitHub/Slack test credentials, OpenCode, Hermes, native macOS CI, and a hosted signed GitHub release were unavailable. No certification is claimed for those targets.
