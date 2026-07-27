# Priority 1 Spike Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This document records the current implementation status of the three Priority 1 spikes:

- S-001 wrapper boundary
- S-002 Postgres lease/recovery
- S-003 local patch-promotion + crash recovery

## S-001 — Wrapper boundary

### Implemented
- `cmd/futurediff/main.go`
- `control-plane/gateway/spike.go`
- `control-plane/gateway/spike_test.go`

### Proven behavior
- `futurediff run` creates a transaction ID and staged worktree.
- one staged command executes inside the worktree boundary.
- effect IDs are persisted in transaction metadata and ledger entries.
- `futurediff inspect` shows the stored staged patch.
- `futurediff commit` applies the stored patch to the source repo without rerunning the staged command.

### Verification
- Go tests pass.
- CLI scenario was executed end-to-end against a temporary git repo.

## S-002 — Postgres lease/recovery

### Implemented
- `control-plane/locks/postgreslease/store.go`
- `control-plane/locks/postgreslease/store_test.go`

### Proven behavior
- worker A can claim a Postgres-backed lease.
- worker A can renew the lease.
- worker B cannot steal an active lease.
- worker B can reacquire the lease after expiry.
- persisted transaction state remains available for recovery after reacquire.

### Verification
- Go tests pass using a real temporary PostgreSQL 18 instance started during test execution.

## S-003 — Local patch-promotion recovery

### Implemented
- `SpikeService.Commit`
- `SpikeService.Recover`
- recovery test in `control-plane/gateway/spike_test.go`

### Proven behavior
- exact staged patch promotion works.
- commit state is persisted before patch application.
- a simulated crash after patch application leaves the transaction in `COMMITTING`.
- `futurediff recover` can confirm the patch was already applied and finalize the transaction deterministically.

### Verification
- Go tests pass.
- CLI recovery scenario was executed against a temporary git repo.

## Verification commands run

```bash
go test ./...
```

And two live CLI scenarios were executed with:
- `futurediff run`
- `futurediff inspect`
- `futurediff commit`
- `futurediff recover`

against temporary git repositories.

## What remains incomplete

Priority 2 is still open:
- disposable Postgres migration preview spike
- GitHub duplicate-recovery spike
- Slack ambiguous-send spike

The first runnable slice still does not include:
- DB migration preview path
- GitHub PR prepare path
- Slack outbox prepare path
- failed verification path

## Current verdict

The bootstrap repo now has real engine behavior, not just planning artifacts.

FutureDiff can already prove:
- staged local execution;
- durable transaction/effect IDs;
- no-second-run patch promotion;
- Postgres lease reacquisition;
- deterministic local recovery after crash during promotion.
