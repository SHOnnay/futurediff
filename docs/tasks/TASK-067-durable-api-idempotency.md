# Task 067 — Durable mutation idempotency and strict request parsing

## Goal
Make retries of local daemon mutations safe and deterministic without relying on clients to know whether the first request completed.

## Implemented

- Optional `Idempotency-Key` on every non-read HTTP request.
- Key namespace is bound to the kernel-authenticated principal.
- Method, path and exact body digest are bound to the key.
- Completed responses up to 1 MiB are stored in SQLite and replayed after restart.
- Reuse with different request material returns HTTP 409.
- Concurrent in-progress reuse returns HTTP 425.
- Server failures release the reservation rather than caching a transient 5xx result.
- Requests and stored responses are limited to 1 MiB.
- Unknown JSON fields and trailing JSON values are rejected.
- Go clients can call `DoIdempotent`.

## Data model

Migration `0011_api_request_safety.sql` adds `api_idempotency_requests` and `api_access_events`.

## Validation

A process-level request produced 201, an identical retry produced 201 with `Idempotency-Replayed: true`, and a changed body with the same key produced 409. The handler executed once.
