# Task 070 — Mutation API audit trail

## Goal
Provide operator evidence of local mutation attempts without storing request bodies, idempotency keys or response content in the access log.

## Implemented

Each mutation attempt records:

- Kernel-authenticated principal identifier.
- Method and normalized API path.
- HTTP status.
- SHA-256 of the idempotency key when present.
- SHA-256 of the exact request material.
- Timestamp and sequence.

The audit includes successful requests, replayed requests, conflicts, in-progress conflicts, malformed keys and oversized requests when the ledger is available.

`futurediff-api-audit` provides aggregate status counts and a bounded recent-event list. No request body or raw key is returned.

## Validation

The process test recorded two 201 outcomes and one 409 conflict. The report contained only digests, not the original key or JSON body.
