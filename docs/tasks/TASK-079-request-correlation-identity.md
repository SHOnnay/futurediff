# Task 079 — Request correlation identity

## Goal
Make local API activity traceable across client output, daemon responses and the durable mutation audit without using request identity as authentication.

## Implemented

Every HTTP request now receives an `X-Request-ID`:

- a valid client value is preserved;
- absent or invalid values are replaced with a random 128-bit identifier;
- the identifier is available in request context;
- the response returns the identifier;
- mutation audit records include it;
- the API audit hash chain protects it against later modification.

Responses also set `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.

Request IDs accept only 8–128 characters from letters, digits, dot, underscore, colon and dash. They are correlation labels, not credentials, authorization claims or idempotency keys.

## Validation

A real daemon request preserved `validation-request-0001` in the response and durable API audit. Modifying the stored request ID caused audit-chain verification to fail in unit testing.
