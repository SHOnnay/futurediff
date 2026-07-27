# Task 037 — Deterministic Offline Ledger Restore

## Goal

Restore a validated SQLite backup without replacing a live database blindly.

## Implemented

- `futurediff-restore` dry-run validation.
- Mandatory expected SHA-256 for apply mode.
- Mandatory confirmation phrase: `RESTORE_FUTUREDIFF_LEDGER`.
- Refusal while the daemon socket exists.
- Candidate-copy validation so the supplied backup is never modified.
- SQLite integrity, migration identity, semantic audit, and event-chain checks.
- Consistent pre-restore online backup of the current ledger.
- Removal of stale WAL/SHM companions after the daemon is stopped.
- Atomic same-directory restored-database publication on supported Unix platforms.
- Directory synchronization after publication.

## Limitation

Windows remains unsupported. Operators must stop the daemon before restore. The pre-restore backup is the rollback artifact; cross-filesystem atomic replacement is not attempted.

## Validation

A live two-transaction ledger was replaced from a one-transaction backup. The restored ledger reopened with integrity, audit, migration, and event-chain validation passing, while a pre-restore backup was preserved.
