# FutureDiff Architecture v15.5 — Release Promotion Overlay

## Added trust boundary

The v15.5 overlay adds an explicit boundary between a locally verified release candidate and an externally certified production launch.

```text
verified source candidate
        |
        v
external evidence intake ---- hosted identity claims policy
        |                               |
        +-------------+-----------------+
                      v
             digest-bound approvals
                      |
             validated exceptions
                      |
                      v
          production promotion decision
                      |
               deployment occurs
                      |
       post-deployment health observation
                      |
       rollback readiness + trigger decision
                      |
                      v
          production launch completion
                      |
             transparency hash chain
```

## New components

- `tools/futurediff_promotion.py`: fail-closed promotion assurance engine.
- External evidence policy and schema.
- Hosted workflow identity claims policy.
- Temporary exception governance.
- Append-only transparency ledger.
- Promotion and launch decision models.
- Post-deployment and rollback evaluators.
- Deterministic promotion evidence bundle.
- GitHub attestation verification wrapper.
- Manual protected-environment workflows for promotion and launch.

## Non-claim

The overlay implements and locally tests the controls. It does not manufacture the real external evidence required by those controls. Production completion remains blocked until real certification, provider, hosted runner, security review, deployment observation, and rollback evidence are supplied and pass.
