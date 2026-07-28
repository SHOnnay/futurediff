# FutureDiff Task 087 — Tamper-evident authorization decision audit

## Objective

Record every RBAC/capability allow or deny decision independently from the existing mutation API audit.

## Implemented

- Ledger migration `0015_authorization.sql`.
- Append-only `authorization_decisions` table.
- Global sequence and SHA-256 previous-digest chain.
- Payload-free fields: principal UID identity, canonical operation ID, optional transaction resource ID, allow/deny result, source, reason code, policy digest, role names, capability digest, request ID, and timestamp.
- New command:

```bash
futurediff-authz-audit --root ~/.futurediff
futurediff-authz-audit --root ~/.futurediff --verify
```

- Main semantic ledger audit now verifies the authorization chain.
- Signed integrity checkpoints include the authorization-chain head.

## Security properties

The chain detects row modification, insertion, deletion, reordering, sequence gaps, or previous-digest changes. Raw capability tokens and policy files are not copied into the ledger.

## Validation

- A modified decision row caused chain verification to fail.
- The live daemon produced five decisions: three allowed and two denied.
- The resulting chain verified successfully with a stable head digest.

## Limitations

The chain is locally tamper-evident, not externally anchored or trusted-timestamped.
