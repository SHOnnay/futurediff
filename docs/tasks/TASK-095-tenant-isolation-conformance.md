# FutureDiff Task 095 — Tenant-isolation conformance suite

## Objective

Provide repeatable release evidence for the transaction ownership and sharing model.

## Command

```bash
futurediff-tenant-conformance
```

## Controls

The suite checks:

1. owner persistence;
2. owner implicit administration;
3. stranger denial;
4. owner read-grant authority;
5. read-grant visibility;
6. read-grant mutation denial;
7. operate-grant upgrade;
8. operate-grant mutation authority;
9. absence of grantable administration;
10. non-owner grant denial;
11. owned/shared/all listing separation;
12. immediate revocation;
13. transaction-access chain validity.

## Result

```text
Passed:     13
Failed:     0
Conformant: true
```

## Live validation

A separate daemon test used real Linux UIDs `0` and `65534` over the Unix socket. It proved scoped listing, non-disclosure, read/operate separation, administration denial, immediate revocation, and audit-chain integrity.
