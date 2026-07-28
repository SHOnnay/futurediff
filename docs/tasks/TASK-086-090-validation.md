# FutureDiff Tasks 086–090 validation

## Static and unit validation

| Check | Result |
|---|---|
| `gofmt` | PASS |
| `go vet ./...` | PASS |
| `go test ./...` | PASS |
| `go test -race ./...` | PASS |
| `go test -cover ./...` | PASS |
| Go commands built | 68 PASS |
| JSON files parsed | 76 PASS |

## Authorization validation

| Check | Result |
|---|---|
| Policy lint | PASS |
| Policy compilation | PASS |
| Unbound UID default denial | PASS |
| Agent-role unsafe-operation rejection | PASS |
| Deterministic route-to-operation matching | PASS |
| Ed25519 capability signing/verification | PASS |
| UID scope mismatch | REJECTED |
| Resource scope mismatch | REJECTED |
| Capability expiry | REJECTED |
| Capability second use | REJECTED |
| Authorization decision chain | PASS |
| Modified authorization event | REJECTED |
| Conformance suite | 12 PASS / 0 FAIL |

## Live daemon workflow

A disposable Linux Git repository and private FutureDiff data root were used.

1. The current UID was bound only to an `agent` role.
2. The agent role created a transaction.
3. Direct `transaction_abort` returned HTTP `403`.
4. An operator signed a five-minute capability bound to that UID, `transaction_abort`, and the exact transaction ID.
5. The capability-authorized abort succeeded and the transaction reached `aborted`.
6. Reusing the same capability failed.
7. The authorization audit contained five decisions: three allowed and two denied.
8. The authorization hash chain verified successfully.

## Existing-system regression

The one-command transaction demo completed a committed future and confirmed that the live checkout remained unchanged.

## Release verification

| Check | Result |
|---|---|
| Release binaries | 68 |
| Offline checks passed | 73 |
| Offline checks failed | 0 |
| Offline checks skipped | 1 |
| SPDX 2.3 SBOM | PASS |
| in-toto/SLSA provenance | PASS |
| Hosted GitHub attestation | SKIPPED |

The hosted-attestation check was skipped because v0.90.0 was generated locally.
