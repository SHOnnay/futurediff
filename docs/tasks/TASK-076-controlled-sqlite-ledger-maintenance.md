# Task 076 — Controlled SQLite ledger maintenance

## Goal
Provide a safe, explicit, offline path for SQLite checkpointing, analysis and compaction without treating routine maintenance as an invisible daemon side effect.

## Implemented

`futurediff-ledger-maintain` now supports:

- dry-run planning by default;
- rejection while the daemon instance lock is held;
- exact confirmation phrase `MAINTAIN_FUTUREDIFF_LEDGER` before applying;
- pre-maintenance semantic ledger audit;
- consistent pre-maintenance SQLite backup with SHA-256 evidence;
- full WAL checkpoint;
- `PRAGMA optimize`, `ANALYZE`, and `VACUUM`;
- post-maintenance integrity and semantic audit;
- before/after file-size reporting.

The command acquires the same kernel lock used by the daemon, preventing a writer from starting during maintenance.

## Validation

A real ledger was planned and maintained. The apply report contained a verified backup digest, both semantic audits were healthy, and the resulting ledger remained readable.
