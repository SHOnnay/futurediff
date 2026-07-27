# ADR-030: Durable external-effect coordinator

**Status:** Accepted

## Decision

External provider mutations must be represented as durable effects with prepared documents, write-ahead attempts, provider receipts, coordinator leases, and explicit reconciliation states.

A transaction containing required effects cannot finalize from local repository publication alone.

## Consequences

- provider calls become transaction material;
- crash recovery can distinguish intent, unknown outcome, and receipt;
- additional schema and adapter complexity is accepted;
- FutureDiff still does not claim a global distributed ACID transaction.
