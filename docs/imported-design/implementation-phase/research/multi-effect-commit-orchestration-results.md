# Multi-Effect Commit Orchestration Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This result records the first real commit path across multiple prepared effect domains.

## Implemented

- `futurediff/integrations/mvpflow/commit.go`
- `futurediff/integrations/mvpflow/commit_test.go`

## Proven behavior

- commit checks GitHub base freshness before outward effects;
- repo promotion still uses the stored staged patch and does not rerun the staged command;
- GitHub PR creation and Slack send run after repo commit;
- ambiguous GitHub and Slack commit calls recover through their adapter recovery paths;
- the orchestrated commit returns durable receipts for all effect domains.

## Verification

- `go test ./...` passes, including `integrations/mvpflow`.

## Why this matters

This is the first proof that FutureDiff is no longer only a preparation system. It now has a narrow but real multi-effect commit path with freshness checks and provider recovery.

## Next useful move

Push this orchestration seam down into a real coordinator-owned transition engine so approval state, reconciliation, and compensation policy stop living only in the integration helper layer.
