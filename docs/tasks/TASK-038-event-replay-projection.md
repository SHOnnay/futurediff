# Task 038 — Event Replay Projection

## Goal

Independently reconstruct transaction and effect status from the tamper-evident event stream and compare it with current SQLite projections.

## Implemented

- `futurediff-replay --transaction <id>`.
- Event-chain verification before replay.
- Transaction-state reconstruction from lifecycle, repository, verification, approval, commit, recovery, abort, and terminal events.
- Effect-state reconstruction from prepare, refresh, commit-intent, rejection, unknown, re-arm, commit, and abort events.
- Approval-digest reconstruction and invalidation handling.
- Machine-readable mismatch findings and nonzero exit status.

## Scope

Replay validates lifecycle projections. It does not rebuild provider resources or rerun commands. Revisions and wall-clock timestamps remain database projections because event payloads were not originally designed as a complete event-sourced database.

## Validation

A complete abort lifecycle and a committed demo lifecycle both replayed to the stored state with matching approval projections.
