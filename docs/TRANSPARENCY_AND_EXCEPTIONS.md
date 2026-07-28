# Transparency Ledger and Risk Exceptions

## Transparency ledger

Production decisions are recorded in an append-only hash chain. Each entry includes:

- sequence number;
- previous entry hash;
- timestamp;
- canonical payload digest;
- entry hash;
- original structured payload.

Any change to an earlier payload, timestamp, sequence, or chain pointer invalidates the ledger. Duplicate payload digests are rejected.

## Risk exceptions

A risk exception is not a bypass. It is a short-lived, approved, scoped record with compensating controls. The default policy requires:

- an allowlisted low or medium risk;
- an allowlisted non-critical scope;
- a detailed rationale;
- at least two compensating controls;
- security and operations approval;
- no owner self-approval;
- automatic expiry within 24 hours.

Production-critical safety, approval binding, effect reconciliation, credential isolation, evidence integrity, or rollback readiness cannot be waived by the supplied default policy.
