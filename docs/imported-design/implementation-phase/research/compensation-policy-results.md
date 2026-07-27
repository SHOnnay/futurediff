# Compensation Policy Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This result records the first narrow compensation path after a partial multi-effect commit.

## Implemented

- `futurediff/adapters/github/prcreate/adapter.go`
- `futurediff/adapters/github/prcreate/adapter_test.go`
- `futurediff/integrations/mvpflow/commit.go`
- `futurediff/integrations/mvpflow/commit_test.go`

## Proven behavior

- GitHub pull requests can be compensated by closing the committed PR;
- a Slack send failure after repo promotion and GitHub PR creation can trigger compensation;
- compensation state is persisted in the per-transaction commit record;
- the compensated path returns an explicit compensation error instead of pretending the transaction simply succeeded.

## Verification

- `go test ./...` passes, including adapter and orchestration coverage.

## Why this matters

This is the first real proof that FutureDiff can respond to an irreversible multi-effect failure with a concrete compensation policy instead of only escalating everything to manual cleanup.

## Next useful move

Add coordinator-owned compensation policy selection so different effect mixes can choose between compensate, reconcile, and manual intervention by rule instead of by one hard-coded branch.
