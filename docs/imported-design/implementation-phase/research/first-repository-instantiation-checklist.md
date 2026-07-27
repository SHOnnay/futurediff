# First Repository Instantiation Checklist

## Goal

This checklist is the first concrete handoff from research into repository bootstrap.

## A. Create repository shape

- [x] create `specs/`
- [x] create `control-plane/`
- [x] create `staging/`
- [x] create `adapters/`
- [x] create `verifier/`
- [x] create `integrations/`
- [x] create `benchmarks/`
- [x] create `examples/`
- [x] create placeholder `ui/`

## B. Land machine-readable contract stubs

- [x] `specs/effectspec/effectspec.schema.json`
- [x] `specs/transactions/transaction-states.json`
- [x] `specs/transactions/effect-states.json`
- [x] `specs/transactions/transition-rules.json`
- [x] `specs/approval/approval-snapshot.schema.json`
- [x] `specs/benchmarks/metrics.schema.json`
- [x] `specs/resources/` normalization fixtures

## C. Land control-plane skeleton

- [x] domain enums
- [x] transaction/effect records
- [x] ledger interface
- [x] lock manager interface
- [x] approval store interface
- [x] adapter registry interface
- [x] reconciler interface

## D. Land adapter workspace skeleton

- [x] `adapters/shared-testkit/`
- [x] `adapters/filesystem/`
- [x] `adapters/git/`
- [x] `adapters/runtime/`
- [x] `adapters/postgres/`
- [x] `adapters/github/`
- [x] `adapters/slack/`

## E. Land verification and benchmark scaffold

- [x] `verifier/contracts/`
- [x] `verifier/runners/`
- [x] `verifier/evidence/`
- [x] `verifier/freshness/`
- [x] benchmark directories for baseline/direct/sandbox/cross-tool/crash-recovery/adversarial

## F. Freeze first runnable slice

- [x] repo code change path
- [x] disposable Postgres migration path
- [x] GitHub PR prepare path
- [x] Slack outbox prepare path
- [x] failed verification path
- [x] recovery path

## G. Do not allow drift

- [ ] no UI-first work
- [ ] no multi-path interception support yet
- [ ] no unsupported adapter promoted as trusted
- [ ] no broad provider expansion before first slice works
