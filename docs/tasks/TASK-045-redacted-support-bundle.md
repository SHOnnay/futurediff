# Task 045 — Redacted Support Bundle

## Goal

Create a portable diagnostic artifact that can be shared without copying the ledger or secret-bearing configuration.

## Delivered

- `futurediff-support-bundle` command.
- Bundle includes build info, doctor report, ledger audit, aggregate metrics, and API contract.
- Home and data-root path replacement.
- Deterministic ZIP entry metadata.
- Per-entry SHA-256 manifest and independent verification.
- Archive traversal, duplicate entry, and size-limit validation.
- Tests inspect decompressed bundle content for private-path leakage.

## Excluded by design

- SQLite ledger bytes.
- Transaction snapshots and patches.
- Credential configuration contents.
- Environment-variable values.
- Provider response bodies.
