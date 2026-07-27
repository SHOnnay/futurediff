# ADR-035 — Durable Effect Dependency DAG

**Status:** Accepted

## Decision

External effects may declare durable same-transaction dependencies. The ledger validates them, verification/approval material includes them, and the coordinator releases an effect only after all dependencies have committed receipts.

## Rationale

Repository publication, PR creation, and notification must occur in causal order without pretending all providers share one transaction.

## Consequences

- effect order is explicit and auditable;
- missing or unresolved dependencies fail closed;
- dependency cycles must remain rejected as the graph evolves;
- compensation remains provider-specific.
