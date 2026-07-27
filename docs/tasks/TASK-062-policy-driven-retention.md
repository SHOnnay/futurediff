# Task 062 — Policy-driven retention

## Goal
Move terminal runtime-artifact pruning from ad hoc duration flags to a versioned, reviewable policy.

## Implemented
- Retention policy v0.1
- Terminal-age threshold
- Candidate-count and byte caps
- Apply-disabled-by-default control
- Existing deterministic prune-plan reuse
- Exact apply confirmation
- `futurediff-retention-policy` command
- JSON Schema and example

## Safety
The policy deletes only managed runtime artifacts for terminal transactions. Durable ledger rows and published FutureDiff Git refs remain.
