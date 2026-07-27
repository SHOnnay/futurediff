# First Benchmark Smoke Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This first smoke layer covers benchmark scenario B2 from the contract:

- file changes followed by verification failure.

It compares:

- direct execution baseline;
- FutureDiff guarded execution.

## Implemented

- `futurediff/benchmarks/smoke/file_change_failure.go`
- `futurediff/benchmarks/smoke/file_change_failure_test.go`

## Proven behavior

- direct execution leaves the source repo changed even though the run fails;
- FutureDiff stages the change, fails verification, aborts, and keeps the source repo unchanged;
- the comparison records duration for both paths;
- the FutureDiff path ends in `ABORTED`.

## Verification

- `go test ./...` passes, including `benchmarks/smoke`.

## Why this matters

This is the first executable proof of the main FutureDiff safety claim for day-one adoption work: a bad staged future can fail without leaking its file mutation into the source repo.

## Next useful move

Broaden the benchmark matrix beyond smoke scenarios so reconciliation, compensation, and approval invalidation all carry comparable exported evidence bundles.
