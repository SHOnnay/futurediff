# Release Promotion

Release promotion is separate from building a release candidate. A locally valid archive is not production-approved until it is bound to authentic external evidence, a trusted hosted-workflow identity, release approvals, and any tightly controlled exceptions.

## Promotion inputs

- approved release-candidate metadata;
- external evidence intake result;
- hosted identity claims policy result;
- digest-bound approval record;
- optional validated risk-exception results.

The approval record must reference the exact source archive SHA-256. Required approval roles are security, operations, and release management. Exceptions are allowed only for explicitly low-impact scopes and must already have passed the exception policy.

## Two-phase process

1. `release-promotion.sh` evaluates pre-deployment production promotion and creates a deterministic promotion bundle.
2. `production-launch.sh` evaluates observed post-deployment health and rollback readiness before declaring launch completion.

```bash
./scripts/release-promotion.sh \
  /secure/evidence \
  external-evidence-specification.json \
  hosted-claims.json \
  release-candidate.json \
  release-approvals.json \
  dist/promotion
```

The output contains the promotion decision, hosted identity result, external evidence result, transparency ledger, GitHub release metadata, bundle, and bundle verification.
