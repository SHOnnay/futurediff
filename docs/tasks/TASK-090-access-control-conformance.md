# FutureDiff Task 090 — Access-control conformance suite

## Objective

Provide a single deterministic regression suite for local authorization policy and delegated capability behavior.

## Implemented

New command:

```bash
futurediff-authz-conformance --policy authorization.json
```

The suite checks:

1. Policy compilation.
2. Deny-by-default configuration.
3. Unbound UID denial across every API operation.
4. Deterministic repeated decisions.
5. Agent-role safety classification.
6. Ephemeral Ed25519 key generation.
7. Capability signing.
8. Valid capability verification.
9. UID binding.
10. Resource binding.
11. Expiry enforcement.
12. Durable one-time-use enforcement.

## Validation

The repository example and the live test policy both completed all 12 checks with zero failures.

## Scope

The suite validates the local policy/capability implementation. It does not certify external identity providers, remote transport, or organization-wide IAM governance.
