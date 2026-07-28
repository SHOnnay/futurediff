# FutureDiff Task 093 — Principal-scoped transaction listing

## Objective

Prevent list endpoints from becoming a cross-tenant metadata disclosure channel.

## Implementation

Added:

```text
GET /v1/transactions
```

The response is filtered by the authorization role's resource scope:

- `owned`: transactions owned by the principal plus transactions explicitly shared with it;
- `all`: all transactions, intended for trusted operators.

Authorization roles now accept:

```json
{
  "resource_scope": "owned"
}
```

Valid values are `owned` and `all`. Missing values normalize to `owned`. Agent-designated roles must use `owned`.

## Security properties

- Filtering is performed in the SQLite query, not after loading every transaction into memory.
- A transaction inaccessible to a principal returns `404` rather than confirming its existence.
- The list response does not include grant records or other principals unless the separate administrative access endpoint is authorized.

## Validation

- A UID owning one transaction saw exactly one transaction.
- A principal with a shared transaction saw owned plus shared rows.
- An all-scope operator saw both disposable validation transactions.
