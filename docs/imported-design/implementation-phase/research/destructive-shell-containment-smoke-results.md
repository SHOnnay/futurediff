# Destructive Shell Containment Smoke Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This smoke layer covers benchmark scenario B1 from the contract:

- destructive shell command.

It compares:

- direct execution baseline;
- FutureDiff contained execution.

## Implemented

- `futurediff/benchmarks/smoke/destructive_shell.go`
- `futurediff/benchmarks/smoke/destructive_shell_test.go`

## Proven behavior

- direct execution deletes a tracked file in the source repo;
- the FutureDiff path runs the same destructive command in a staged worktree;
- the source repo remains intact before commit;
- the staged patch still captures the destructive deletion for inspection;
- the contained transaction ends in `AWAITING_APPROVAL`.

## Verification

- `go test ./...` passes, including `benchmarks/smoke`.

## Why this matters

This is the first executable proof that FutureDiff can contain a destructive shell effect without pretending the effect never existed. The damage is inspectable in staging and absent from the source repo until an explicit commit path is chosen.

## Next useful move

Carry this containment check into the Docker-backed execution mode so the same destructive-command proof exists for both host-shell and containerized staged runtimes.
