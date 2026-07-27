# Stale GitHub Drift Smoke Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This smoke layer covers benchmark scenario B4 from the contract:

- stale GitHub base branch / drift.

It compares:

- a direct create path with no freshness check;
- a FutureDiff path with pinned-base freshness verification.

## Implemented

- `futurediff/adapters/github/prcreate/adapter.go`
- `futurediff/adapters/github/prcreate/adapter_test.go`
- `futurediff/benchmarks/smoke/stale_github_base.go`
- `futurediff/benchmarks/smoke/stale_github_base_test.go`

## Proven behavior

- the GitHub adapter can now check current base-branch freshness against a prepared expected SHA;
- a stale base branch is detected before PR creation;
- the direct path still creates the PR on the stale base;
- the FutureDiff path blocks before any PR create call is made.

## Verification

- `go test ./...` passes, including adapter and smoke coverage.

## Why this matters

This is the first executable proof that approval/preparation can actually be invalidated by upstream branch drift instead of being treated like a fuzzy suggestion.

## Next useful move

Carry the same freshness discipline into broader commit orchestration and future approval-snapshot invalidation tests.
