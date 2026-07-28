# FutureDiff Task 086 — UID-based local RBAC

## Objective

Add deny-by-default authorization above the existing Linux Unix-peer authentication boundary. Peer identity proves which local UID connected; this task decides which API operations that UID may invoke.

## Implemented

- New `internal/authorization` policy compiler and deterministic authorizer.
- Versioned policy with roles, UID bindings, explicit operation IDs, and `default: deny`.
- Operation names are validated against the canonical API contract.
- Agent-designated roles are rejected if they include any endpoint marked `agent_safe: false`.
- New daemon flag: `--authorization-policy`.
- Authorization executes after kernel peer authentication and before rate limiting, idempotency reservation, or handlers.
- Health output reports the policy digest without exposing policy contents.
- Installer/systemd/launchd generation supports the policy path.
- Configuration linting recognizes `authorization-policy`.

## Commands

```bash
futurediff-authz --policy authorization.json
futurediff-authz --policy authorization.json --uid 1000 --method POST --path /v1/transactions/tx-1/abort
```

## Security properties

- Unbound UIDs are denied.
- Unknown operations are denied.
- Policy defaults other than `deny` are rejected.
- Duplicate roles, bindings, or operation grants are rejected.
- Policy files must not be group/world writable.
- Authorization decisions are deterministic and bound to the canonical operation ID, not an arbitrary path string.

## Validation

- Unit tests cover default denial, role grants, unknown UIDs, and unsafe agent-role rejection.
- Live daemon validation allowed transaction creation for the configured agent role and returned HTTP `403` for an ungranted abort.

## Limitations

This is local Unix-UID RBAC. It is not network identity, enterprise SSO, LDAP, or cloud IAM.
