# Coordinator Approval State Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This result records the first coordinator-owned approval state store for the bootstrap repo.

## Implemented

- `futurediff/control-plane/coordinator/approvalstate/store.go`
- `futurediff/control-plane/coordinator/approvalstate/store_test.go`

## Proven behavior

- approval snapshot refs can be stored by transaction ID;
- approved state can be loaded through the coordinator-facing store;
- invalidation persists as coordinator-owned state with a reason;
- invalidated approval no longer loads as an active approved snapshot.

## Verification

- `go test ./...` passes, including `control-plane/coordinator/approvalstate`.

## Why this matters

This moves approval state out of loose helper-only logic and into a real control-plane-owned store boundary, which is where maintainable invalidation and audit behavior belongs.

## Next useful move

Wire coordinator-owned approval state into transaction transitions so approval invalidation can drive `AWAITING_APPROVAL -> ACTIVE` from one durable source of truth.
