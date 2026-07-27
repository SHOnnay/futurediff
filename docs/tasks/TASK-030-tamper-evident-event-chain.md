# Task 030 — Tamper-evident event chain

## Implemented

- SQLite migration 0009 adds `previous_event_hash` and `event_hash`.
- Existing event rows are deterministically backfilled.
- New transaction and effect events use the same chained insertion path.
- Ledger opening fails on an already-chained mismatch.
- `VerifyEventChains` returns a machine-readable report.
- Tests prove payload-digest tampering is detected and chains remain independent per transaction.

## Security boundary

The chain detects accidental or uncoordinated modification. It is not an external signature or transparency log and cannot defeat an attacker who can rewrite the database and recompute all hashes.
