# Task 017 — SQLite ledger integrity, migration identity, and backup

## Goal

Harden the local transaction ledger against unsafe file-copy backups, silent migration changes, and undetected database corruption.

## Implemented

- SQLite `integrity_check` support.
- Blocking WAL checkpoint support.
- Consistent online backups using SQLite's backup API.
- Atomic backup publication with mode `0600`.
- Backup SHA-256 digest and size recording.
- `migration_artifacts` table with embedded migration filename and SHA-256 verification on every repository open.
- `futurediff-admin` command for ledger health and backup.
- Health metrics for migrations, transactions, and unresolved transactions.
- Backup reopening and integrity tests.

## Validation

The Task 018 demo ledger passed `integrity_check`, contained eight migrations and one committed transaction, and reported zero unresolved transactions. Its consistent backup reopened successfully and passed integrity validation.

## Remaining work

Add disk-full fault injection, corruption fixtures, restore drills across released schema versions, and a final maintained SQLite driver decision.
