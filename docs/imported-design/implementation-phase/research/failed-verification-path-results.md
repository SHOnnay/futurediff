# Failed Verification Path Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This result records the first runnable proof for the benchmark-critical scenario:

- staged repository diff exists;
- verification fails before approval/commit;
- the transaction aborts;
- the source repo is left unchanged;
- stored evidence remains inspectable.

## Implemented

- `futurediff/control-plane/gateway/spike.go`
- `futurediff/control-plane/gateway/spike_test.go`
- `futurediff/cmd/futurediff/main.go`

## Proven behavior

- `SpikeService.RunWithOptions` accepts an optional staged verification command.
- Verification runs inside the staged worktree after the patch is captured.
- Verification output is persisted to `verification.log` inside the transaction evidence directory.
- Verification failure transitions the transaction through `VERIFYING` -> `ABORTING` -> `ABORTED`.
- The staged patch remains available through `futurediff inspect` after abort.
- `futurediff commit` rejects aborted transactions.
- The source repo does not receive the staged patch after verification failure.
- Passing verification transitions the filesystem effect to `VERIFIED` before approval.

## Verification

- `go test ./...` passes.
- A live CLI scenario was executed with:
  - `futurediff run --verify-shell ...`
  - aborted transaction output
  - preserved staged patch through `futurediff inspect`

## Why this matters

This is the first direct proof that FutureDiff can block promotion after a bad staged future without rerunning the agent and without leaking the staged patch into the source repository.

## Next useful move

Keep this verification gate as the baseline safety invariant while broader coordinator-owned approval and compensation policy are wired in.
