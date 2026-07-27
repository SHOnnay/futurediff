# Task 069 — Resource quota enforcement

## Goal
Bound local resource amplification before a buggy or adversarial agent creates excessive transactions, effects, executions or repository material.

## Implemented

Versioned quota policy `0.1` controls:

- Open transactions.
- External effects per transaction.
- Runtime executions per transaction.
- Patch bytes.
- Changed paths.
- Verification checks.

The daemon loads an optional `--quota-policy`; safe defaults apply otherwise. Enforcement occurs before allocating the next bounded resource. Oversized captured patches are removed and the transaction stays active.

The `futurediff-quota` command reports policy and current use without exposing transaction content.

## Validation

- A second open transaction was rejected at a limit of one.
- An oversized patch was rejected before sealing.
- Policy validation rejects zero and negative limits.
