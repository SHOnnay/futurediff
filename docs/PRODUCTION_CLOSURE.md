# Production Closure

FutureDiff distinguishes implementation completion from externally proven production completion.

The closure layer requires twelve independent result types: canonical repository integration, historical archive completeness, evidence freshness, external certification campaign coverage, independent security review, measured load/soak evidence, disaster-recovery evidence, change freeze approval, credential metadata readiness, real deployment smoke tests, real rollback exercise, and operational sign-off.

The final decision is fail-closed. Example, synthetic, expired, incomplete, self-approved, digest-unbound, or missing results cannot set `production_complete=true`.

```bash
./scripts/production-closure.sh \
  /path/to/canonical/futurediff \
  /path/to/base-source.zip \
  /path/to/historical-zips \
  dist/closure
```

The packaged examples validate evaluator behavior only. They deliberately do not represent real external certification.
