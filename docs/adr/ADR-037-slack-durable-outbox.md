# ADR-037 — Slack Messages Use a Durable Outbox

**Status:** Accepted

## Decision

Slack messages are stored as prepared outbox effects and released only after approval and dependency completion. Stable client and metadata markers support reconciliation.

## Rationale

Sending a message is an immediate social side effect and cannot be safely treated like a local file mutation.

## Consequences

- Slack commits late in the effect order;
- transport uncertainty becomes `UNKNOWN`;
- recovery searches provider state before any retry;
- deletion is not represented as true rollback.
