# FutureDiff Task 100 — Tenant governance conformance suite

## Objective

Provide deterministic local release evidence for access expiry, cleanup, tenant quotas and privacy-minimized inventory.

## Command

```bash
futurediff-tenant-governance --dir /absolute/disposable-directory
```

## Controls

The suite checks:

1. expiring grant creation;
2. authority before expiry;
3. denial after expiry;
4. scoped-list removal after expiry;
5. deterministic cleanup planning;
6. cleanup confirmation enforcement;
7. expired-row deletion;
8. row absence after cleanup;
9. access-chain validity after cleanup;
10. tenant-quota policy validation;
11. per-owner open-transaction enforcement;
12. per-transaction active-grant enforcement;
13. per-recipient shared-transaction enforcement;
14. principal redaction in inventory;
15. deterministic inventory digest.

## Result

```text
Passed:     15
Failed:     0
Conformant: true
```

## Claims boundary

The suite validates local SQLite, Unix-principal and policy behavior. It does not certify enterprise IAM, distributed tenancy, external identity providers, containers, GitHub, Slack, agent runtimes, macOS or hosted release signing.
