# Task 042 — Policy Explanation and Simulation

## Goal

Allow developers and reviewers to understand a verification contract before an agent runs it.

## Delivered

- Deterministic dependency ordering.
- Human-readable explanation for each check.
- Warnings for local commands, missing command timeouts, and optional checks.
- Optional simulated check results with dependency blocking and final PASS/FAIL/ERROR calculation.
- `futurediff-policy-explain` command.

## Safety

Simulation is explanatory only. It cannot write verification results to the ledger or move a transaction to READY.
