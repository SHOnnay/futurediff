# ADR-081 — Mutation retries are durably idempotent

**Decision:** A client-supplied idempotency key is bound to principal, method, path and body digest. Completed non-5xx responses are stored and replayed; mismatched reuse fails closed.
