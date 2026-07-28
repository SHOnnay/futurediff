# FutureDiff Task 089 — Authorization policy explanation and simulation

## Objective

Let operators inspect a policy decision before deployment without running the daemon or mutating the ledger.

## Implemented

`futurediff-authz` validates the complete policy and optionally simulates a UID, HTTP method, and concrete path against the current API contract.

Output includes:

- policy validity and digest;
- role and binding counts;
- matched canonical endpoint and operation ID;
- extracted transaction/resource ID;
- allowed/denied result;
- effective roles;
- deterministic reason code.

Example:

```bash
futurediff-authz \
  --policy authorization.json \
  --uid 1000 \
  --method POST \
  --path /v1/transactions/tx-123/commit
```

## Validation

The live policy simulation correctly reported `default_deny` for an agent UID attempting `transaction_abort`.

## Limitations

Simulation is read-only and does not predict dynamic transaction state, provider behavior, rate limits, storage pressure, or capability availability.
