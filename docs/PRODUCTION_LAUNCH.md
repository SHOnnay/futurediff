# Production Launch Completion

FutureDiff considers a launch externally complete only when all of the following are true:

1. the exact release has an approved external production-promotion decision;
2. post-deployment health evidence passes policy;
3. rollback readiness passes policy;
4. no rollback trigger is active;
5. the runbook is acknowledged;
6. on-call coverage is confirmed;
7. release communications are ready.

The repository default `config/production-launch-policy.json` intentionally has all operator confirmations set to `false`. An operator must provide a separate reviewed policy record with explicit confirmations. This prevents a repository fixture from silently authorizing a real production launch.

```bash
./scripts/production-launch.sh \
  promotion-decision.json \
  postdeploy-health.json \
  rollback-readiness.json \
  reviewed-production-launch-policy.json \
  dist/launch
```

The resulting `production-launch.json` is the only artifact in this overlay that can set `production_complete=true`, and only after real external and post-deployment evidence passes.
