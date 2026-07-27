# Task 036 — Transaction Forensic Export

## Goal

Export one durable FutureDiff transaction into a portable, independently verifiable `.futurepack` without copying provider secret values.

## Implemented

- `futurediff-export --transaction <id> --output <file.futurepack>`
- Durable transaction, workspace, patch, verification, effect, receipt, attempt, runtime, approval, event-chain, credential-access decision, and retention projections.
- Exact patch artifact when it is still present.
- Content-addressed archive entries.
- Archive path, duplicate-entry, duplicate-reference, size, digest, and manifest validation.
- Defense-in-depth key and token-pattern redaction.
- `futurediff-export --verify <file.futurepack>`.

## Security boundary

Credential IDs and access decisions are evidence and remain visible. Credential values, source references, authorization headers, bearer tokens, GitHub token patterns, and Slack token patterns are redacted. The exporter does not claim it can recover a secret that an unsafe third-party adapter embedded in arbitrary binary evidence; built-in adapters must continue to prevent that at ingestion.

## Validation

- Unit export and verification test.
- Nested JSON token redaction test.
- Real committed demo transaction exported with two artifacts.
- Archive re-verification passed.
