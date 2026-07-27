# Task 059 — Incident reconstruction report

## Delivered
- `internal/incident`
- `futurediff-incident`
- Combined transaction diff, event timeline, projection replay, effect states, receipts, and audit findings
- Deterministic severity and recommended actions
- JSON and Markdown output
- Digest-bound report
- Incident report schema

## Security boundary
The report is read-only and excludes raw provider responses, credential values, and patch bodies.

## Validation
A committed demo transaction reconstructed with a valid replay, informational severity, and stable report digest.
