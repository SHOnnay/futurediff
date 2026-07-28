# FutureDiff Task 092 — Transaction sharing grants

## Objective

Allow controlled collaboration without granting another principal full transaction administration.

## Permission model

- `read`: view transaction, events, effects, and other read-only transaction projections.
- `operate`: includes `read` and permits agent-safe transaction mutations.
- `admin`: implicit for the owner and all-scope operators only; it is never grantable.

## API

```text
GET    /v1/transactions/{id}/access
PUT    /v1/transactions/{id}/access/{principalID}
DELETE /v1/transactions/{id}/access/{principalID}
```

CLI:

```bash
futurediff access-list <transaction-id>
futurediff access-grant <transaction-id> uid:1001 read
futurediff access-grant <transaction-id> uid:1001 operate
futurediff access-revoke <transaction-id> uid:1001
```

## Enforcement

- Only the owner or a role with `resource_scope: all` can grant or revoke.
- Owners cannot grant themselves redundant access.
- `admin` grants are rejected.
- Revocation takes effect on the next request.
- RBAC operation permission is still required in addition to resource access.

## Validation

- Read grants allowed retrieval but returned `404` for mutation attempts.
- Operate grants allowed sealing an exact staged future.
- Shared principals could not inspect or change access grants.
- Revocation immediately removed visibility.
