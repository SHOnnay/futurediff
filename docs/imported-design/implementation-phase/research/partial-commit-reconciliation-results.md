# Partial-Commit Reconciliation Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This result records the first narrow reconciliation path for a crash after the repo commit and first external effect.

## Implemented

- `futurediff/integrations/mvpflow/commit.go`
- `futurediff/integrations/mvpflow/commit_test.go`

## Proven behavior

- commit progress is persisted in a durable per-transaction commit record;
- a crash after GitHub receipt persistence can be reconciled;
- repo promotion is not repeated blindly;
- GitHub create is not duplicated during reconciliation;
- pending Slack send is resumed and completed after reconciliation.

## Verification

- `go test ./...` passes, including `integrations/mvpflow`.

## Why this matters

This is the first concrete proof that FutureDiff can recover from a real partial-commit boundary instead of only talking about it in state-machine prose.

## Next useful move

Extend reconciliation toward explicit compensation and manual-intervention paths once more external effect types join the commit plan.
