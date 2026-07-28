# FutureDiff Task 094 — Tamper-evident transaction-access audit

## Objective

Make ownership creation, grants, grant upgrades, and revocations independently auditable.

## Event chain

Each event binds:

- global sequence;
- event identity;
- transaction identity;
- actor principal;
- subject principal;
- action and permission;
- request correlation ID when available;
- timestamp;
- previous event digest.

The resulting SHA-256 digest becomes the next event's predecessor.

## Command

```bash
futurediff-access-audit --root "$HOME/.futurediff" --verify
```

Without `--verify`, the command returns bounded recent events plus chain status and head digest.

## Integration

- The semantic ledger audit verifies the chain.
- Signed integrity checkpoints bind the transaction-access chain head.
- Support and readiness flows inherit the result through the semantic audit.

## Claims boundary

This provides local tamper evidence. It is not an externally timestamped transparency log and does not prevent a fully privileged attacker from rewriting the database and recomputing every digest.

## Validation

A test modified `subject_principal_id` directly in SQLite; complete-chain verification rejected the altered event.
