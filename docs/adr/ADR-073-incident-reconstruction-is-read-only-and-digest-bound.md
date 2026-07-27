# ADR-073: Incident reconstruction is read-only and digest-bound

## Decision
Incident reports combine timeline, diff, event replay, effect state, and audit findings without mutating the ledger.

## Rationale
Forensic analysis must not alter the evidence it is evaluating.

## Consequences
Recommendations are deterministic guidance only and do not execute recovery actions.
