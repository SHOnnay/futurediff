# Task 073 — Principal rate and concurrency limits

## Goal
Bound request amplification and concurrent mutation pressure at the authenticated local API boundary.

## Implemented

A versioned rate policy controls:

- read requests per minute;
- read burst capacity;
- mutation requests per minute;
- mutation burst capacity;
- concurrent mutations per authenticated principal.

The daemon uses independent token buckets for reads and mutations. Mutation slots are held until the request handler returns. Rejected requests receive HTTP `429`, a `Retry-After` header, and a payload-free API audit event.

Safe defaults are enabled even when no custom policy is supplied. A custom policy is loaded with `--rate-policy`. `futurediff-rate-policy` validates and displays policies and includes a self-test mode.

## Limitations

Rate state is intentionally in memory and resets on daemon restart. Durable idempotency remains the correctness mechanism; rate limiting is an abuse-control boundary rather than a transaction guarantee.

## Validation

A live daemon configured with a read burst of two returned `200`, `200`, then `429`, while a mutation still succeeded through its separate bucket.
