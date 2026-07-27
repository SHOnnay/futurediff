# FutureDiff Task 082 — Durable idempotency retention

## Objective

Bound durable API-response storage while preserving the independent payload-free mutation audit chain.

## Delivered

- `internal/idempotencygc` policy, planner, and apply engine.
- `futurediff-idempotency-gc` command.
- Separate retention thresholds for `completed` and stale `in_progress` records.
- Candidate-count limit.
- Offline daemon-lock requirement for apply.
- Dry-run default and exact confirmation: `DELETE_EXPIRED_FUTUREDIFF_IDEMPOTENCY_RECORDS`.
- Plans expose only principal/key SHA-256 identities.
- Durable aggregate `idempotency_gc_actions` record.

## Safety properties

Request and response bodies are not emitted. Raw authenticated principals and idempotency keys are not emitted. Delete predicates include principal, key, request digest, state, and exact update timestamp so changed rows fail closed.

## Validation

Tests covered redaction, wrong-confirmation rejection, completed-record deletion, and preservation of API audit evidence. A real idempotent create response was aged and removed through the command.
