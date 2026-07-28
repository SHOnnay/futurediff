# FutureDiff Task 088 — One-time signed operator capabilities

## Objective

Allow an operator to delegate exactly one otherwise-denied unsafe API operation without broadly assigning an operator role to the requesting UID.

## Implemented

- New `internal/capability` Ed25519 token format.
- Maximum lifetime: 15 minutes.
- Token binds subject UID, canonical operation ID, optional transaction/resource ID, operator/key identity, issue/expiry times, random nonce, and unique capability ID.
- Compact base64url encoding for the `X-FutureDiff-Capability` header.
- Durable one-time-use table `authorization_capability_uses`.
- Capability use is accepted only for endpoints marked `agent_safe: false`.
- New daemon flag: `--capability-keyring`.
- New command:

```bash
futurediff-capability sign \
  --private operator-private.json \
  --uid 1000 \
  --operation transaction_abort \
  --resource tx-123 \
  --ttl 5m \
  --output capability.json

futurediff-capability verify \
  --keyring operator-keyring.json \
  --input capability.json \
  --uid 1000 \
  --operation transaction_abort \
  --resource tx-123
```

- Main CLI accepts `--capability-file`.

## Security properties

- UID mismatch, operation mismatch, resource mismatch, expiry, future issuance, disabled key, invalid signature, and replay are rejected.
- The durable ledger records only a SHA-256 capability digest, never the token or signature contents.
- A capability is consumed before the protected handler runs; it cannot be retried as a reusable credential.

## Validation

The live daemon rejected an abort with HTTP `403`, accepted the same abort with a valid scoped capability, moved the transaction to `aborted`, and rejected replay of the same capability.
