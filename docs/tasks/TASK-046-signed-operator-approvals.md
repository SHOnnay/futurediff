# Task 046 — Signed operator approvals

## Objective

Bind human approval to an exact FutureDiff transaction using an expiring Ed25519 signature rather than an unauthenticated identity string.

## Delivered

- `internal/operatorapproval` key generation, signing, verification, keyring loading, and signature-reference generation.
- `futurediff-approval generate|sign|verify`.
- Daemon flags `--approval-keyring` and `--require-signed-approvals`.
- `futurediff approve-signed <transaction-id> <envelope.json>`.
- Ledger persistence through the existing `signature_ref` and `expires_at` approval fields.
- Installer support for approval-keyring and signed-approval flags.
- Health output describing whether a keyring is configured and whether signatures are mandatory.
- JSON Schemas for approval envelopes and trusted keyrings.

## Security decisions

Private keys are stored only in a `0600` operator file. Keyrings must not be group/world writable. An envelope signs transaction ID, transaction digest, approver, key ID, issue time, expiry, and a random nonce. TTL is limited to 24 hours. Signed approval fails when transaction material changes, the envelope expires, the key is disabled, the approver differs, or the signature is invalid.

## Validation

Unit tests cover signature verification, tampering, expiry, and file permissions. Application tests prove that strict mode rejects an unsigned approval and stores a non-secret signature reference for a valid signed approval.
