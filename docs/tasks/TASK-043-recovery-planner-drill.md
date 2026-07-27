# Task 043 — Recovery Planner Drill

## Goal

Make ambiguous external-effect recovery rules executable and reviewable.

## Delivered

- Deterministic recovery planner for committing, compensating, and reconciliation states.
- Explicit actions for status query, re-arm, stale marking, finalization, compensation, and manual intervention.
- Blind retry is false for every ambiguous-provider scenario.
- Five built-in safety scenarios through `futurediff-recovery-drill`.
- Custom scenario JSON support.

## Limitation

The planner is a decision oracle and release test. Provider status queries and ledger state changes remain owned by the durable coordinator.
