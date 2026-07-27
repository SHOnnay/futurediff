# Coordinator Transition Wiring Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This result records the first coordinator-owned transition engine for approval, commit, reconciliation, compensation, and manual-intervention transaction state changes.

## Implemented

- `futurediff/control-plane/coordinator/engine.go`
- `futurediff/control-plane/coordinator/engine_test.go`
- `futurediff/control-plane/coordinator/interfaces.go`

## Proven behavior

- approval can move a transaction from `AWAITING_APPROVAL` to `READY_TO_COMMIT` through a coordinator-owned engine;
- approval invalidation can move a transaction from `AWAITING_APPROVAL` or `READY_TO_COMMIT` back to `ACTIVE`;
- commit can move a transaction from `READY_TO_COMMIT` to `COMMITTING` and then to `COMMITTED`;
- reconciliation can move a transaction from `COMMITTING` to `RECONCILING` and then back into a durable terminal path;
- compensation can move a transaction from `COMMITTING` or `RECONCILING` to `COMPENSATING` and then `COMPENSATED`;
- manual-intervention escalation can move a transaction from `RECONCILING` or `COMPENSATING` to `FAILED_MANUAL_INTERVENTION`;
- effect states are updated alongside the transaction transition;
- coordinator-owned ledger entries are emitted for approval, invalidation, commit, reconciliation, compensation, and manual-intervention transitions.

## Verification

- `go test ./...` passes, including `control-plane/coordinator`.
- `go vet ./...` passes.

## Why this matters

This is the first real control-plane transition wiring in the repo. Approval, commit, reconciliation, compensation, and manual intervention are no longer only helper-level concepts; they now have a coordinator-owned engine boundary.

## Next useful move

Push these transition methods behind durable primary storage and policy-owned orchestration so the bootstrap repo stops depending on in-memory wiring for critical coordinator behavior.
