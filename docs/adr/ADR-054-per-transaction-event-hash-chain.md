# ADR-054: Per-transaction event hash chain

## Decision

Every ledger event is linked to the previous event for the same transaction. The hash covers the previous hash, event identity, transaction/effect identity, event type, payload digest, fencing token, and timestamp. Existing event rows are backfilled during migration and verified whenever the repository opens.

## Consequences

Accidental row modification, deletion, insertion, and reordering become detectable. This is tamper-evident, not tamper-proof: an attacker with unrestricted database write access and knowledge of the algorithm could recompute the chain. External anchoring or signatures remain production hardening work.
