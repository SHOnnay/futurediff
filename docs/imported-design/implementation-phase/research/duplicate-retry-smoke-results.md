# Duplicate Retry Smoke Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This smoke layer covers benchmark scenario B5 from the contract:

- duplicate API retry.

It compares:

- a naive direct retry baseline;
- a FutureDiff recovery-first path.

## Implemented

- `futurediff/benchmarks/smoke/duplicate_api_retry.go`
- `futurediff/benchmarks/smoke/duplicate_api_retry_test.go`

## Proven behavior

- a naive direct retry after an ambiguous GitHub PR timeout can create duplicates;
- the FutureDiff path performs one create attempt, then resolves through recovery;
- the FutureDiff path ends with one durable pull request and a recovered receipt.

## Verification

- `go test ./...` passes, including `benchmarks/smoke`.

## Why this matters

This is the first benchmark proof that FutureDiff does not need to choose between “retry blindly” and “give up.” It can recover the prior durable effect and avoid duplicate outward state.

## Next useful move

Expand duplicate-retry coverage to other adapters with weaker provider semantics, especially Slack and future DB-side effects.
