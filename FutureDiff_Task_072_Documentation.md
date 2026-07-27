# Task 072 — Tamper-evident API audit chain

## Goal
Make payload-free mutation-access evidence detect row modification, deletion, insertion, or reordering.

## Implemented

Every API access event now stores:

- monotonically increasing sequence;
- event identity;
- principal, method, normalized path, and HTTP status;
- request and idempotency-key digests when available;
- previous-event digest;
- canonical event digest.

The chain is calculated inside the same SQLite write transaction as the event insertion. Existing v0.70.0 audit rows are backfilled during migration 0012. Once a row already has a digest, migration refuses to silently rewrite an invalid chain.

`futurediff-api-audit --verify` verifies the full chain and returns a nonzero exit code on failure. Normal summaries include chain validity and the current head digest.

## Security boundary

The chain is tamper-evident, not externally anchored. An attacker with unrestricted database write access and knowledge of the algorithm could rewrite all rows and recompute the chain. External transparency-log anchoring remains production work.

## Validation

A real mutation and a rate-limit rejection produced a valid chain. Modifying the first row's status code caused repository open/verification to fail closed.
