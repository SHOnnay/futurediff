# FutureDiff Task 096 — Expiring transaction access grants

## Objective

Allow owners and all-scope operators to delegate temporary transaction visibility or agent-safe operation without creating an indefinite share.

## Implementation

The transaction access request accepts an optional duration:

```json
{
  "permission": "read",
  "expires_in_seconds": 3600
}
```

The CLI accepts the same duration as the optional fifth argument:

```bash
futurediff access-grant <transaction> <principal> read 3600
```

Rules:

- `0` or omission creates a non-expiring grant;
- negative durations are rejected;
- an expiry must be in the future;
- a grant cannot exceed 30 days;
- updating an existing grant replaces its expiry;
- expired grants no longer satisfy read or operate checks;
- principal-scoped transaction listing excludes expired shares;
- access-list output retains the stored expiry and reports `active: false` after expiration.

## Security boundary

Expiration removes authority immediately during authorization checks. It does not delete the historical row automatically; deletion is a separate offline operation in Task 097.

## Validation

A live daemon accepted a one-second read grant. After expiration, the grant became inactive and stopped occupying the configured active-grant quota.
