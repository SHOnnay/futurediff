# FutureDiff Task 091 — Durable transaction ownership

## Objective

Bind every newly created transaction to the kernel-authenticated local principal that created it.

## Implementation

- Added migration `0016_transaction_ownership.sql`.
- Added `owner_principal_id` to the durable transaction projection and public transaction model.
- The daemon passes the `SO_PEERCRED`-derived principal into transaction creation.
- Local non-daemon callers retain a documented `local:operator` compatibility identity.
- The owner receives implicit read, operate, and administrative authority; no redundant owner grant is stored.
- Transaction creation appends an entry to the independent transaction-access event chain.

## Security properties

- Ownership is not accepted from request JSON or an HTTP identity header.
- Ownership cannot be transferred by the sharing API.
- Existing pre-migration rows receive `legacy:unowned`; they remain available to all-scope operators and offline administration.
- The semantic ledger audit rejects empty owners and redundant explicit owner grants.

## Validation

- Repository tests verified owner persistence and implicit administrative access.
- API tests verified UID `1000` and UID `1001` create transactions with different owners.
- The live Linux daemon test verified owner identity from actual kernel peer credentials.
