# Approval Invalidation Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This result records the first runnable stale-approval path.

## Implemented

- `futurediff/integrations/mvpflow/approval.go`
- `futurediff/integrations/mvpflow/approval_test.go`
- `futurediff/integrations/mvpflow/commit.go`

## Proven behavior

- FutureDiff can capture an approval snapshot for a prepared cross-tool result;
- approval validation checks transaction fingerprint, prepared fingerprints, and pinned GitHub base SHA;
- a materially changed prepared state invalidates the approval before commit;
- invalidated approval blocks repo promotion and outward GitHub/Slack effects.

## Verification

- `go test ./...` passes, including `integrations/mvpflow`.

## Why this matters

This is the first real proof that approval in FutureDiff means approval of an exact prepared future, not a vague approval of intent.

## Next useful move

Push approval invalidation deeper into coordinator-owned state transitions and exported approval artifacts.
