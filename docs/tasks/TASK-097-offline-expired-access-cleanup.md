# FutureDiff Task 097 — Offline expired access cleanup

## Objective

Provide a bounded and reviewable way to remove expired transaction-share rows without weakening the transaction-access evidence chain.

## Command

```bash
futurediff-access-cleanup \
  --root "$HOME/.futurediff" \
  --policy access-cleanup-policy.json
```

Apply mode requires:

```bash
--apply --confirm DELETE_EXPIRED_FUTUREDIFF_ACCESS_GRANTS
```

## Safety controls

- the daemon must be stopped;
- the exclusive daemon lock is acquired;
- dry-run is the default;
- the policy can prohibit apply;
- an optional grace period prevents immediate cleanup at the expiry boundary;
- candidate count is bounded;
- the plan binds policy digest, expiry cutoff, candidate identities, permissions and timestamps;
- principal identities are represented by SHA-256 digests in the plan;
- every candidate is revalidated inside one SQLite transaction;
- changed or renewed grants fail the apply operation;
- each deletion appends a `revoked` access-chain event bound to the cleanup plan digest.

## Audit integration

The semantic ledger audit warns when expired grants remain pending cleanup. Successful cleanup removes the warning while preserving a valid access-event hash chain.

## Validation

The live test identified one expired grant, rejected an apply-disabled policy, rejected an incorrect confirmation phrase, deleted exactly one row with the correct confirmation, and verified the resulting access chain.
