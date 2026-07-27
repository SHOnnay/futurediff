# Task 049 — Offline upgrade rehearsal

## Objective

Prove that the current binary can migrate an existing ledger before the operator upgrades the live installation.

## Delivered

- `futurediff-upgrade-rehearsal`.
- Daemon-offline requirement.
- SQLite online backup into a temporary rehearsal directory.
- Current migration application on the copied ledger only.
- SQLite health, event-chain, and semantic audit checks.
- Before/after migration, transaction, and unresolved-state counts.
- Source-ledger SHA-256 comparison.

## Success criteria

The source ledger remains byte-identical, transaction and unresolved counts remain unchanged, the migration count never decreases, and the upgraded clone passes semantic audit.
