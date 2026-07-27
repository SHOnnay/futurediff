# Migration Failure Smoke Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This smoke layer covers benchmark scenario B3 from the contract:

- migration failure.

It compares:

- direct real-DB execution baseline;
- FutureDiff preview-first execution.

## Implemented

- `futurediff/benchmarks/smoke/migration_failure.go`
- `futurediff/benchmarks/smoke/migration_failure_test.go`

## Proven behavior

- direct execution can leave a real database partially changed before failing;
- the FutureDiff path fails during disposable preview before touching the real database;
- the FutureDiff path blocks the flow before GitHub or Slack network calls occur.

## Verification

- `go test ./...` passes, including `benchmarks/smoke`.

## Why this matters

This is the first executable proof that bad migrations are not just “noticed” but actually stopped before they leak into a real database or fan out into notifications.

## Next useful move

Add reconciliation and compensation coverage for multi-step database-related failures once real Postgres-side commit orchestration moves beyond preview-first blocking.
