# Task 048 — Transaction diff summary

## Objective

Provide a compact review artifact showing what a staged future changes and which external effects will be released.

## Delivered

- `internal/diffsummary` deterministic summary projection.
- `futurediff-diff --format markdown|json`.
- Changed path list, patch digest, Git tree, base revision, verification outcome, external effect ordering and status, approval/receipt/runtime/event counts, and warnings.
- Summary SHA-256 digest.
- No patch body, provider response content, or credential value is included.

## Validation

Tests prove deterministic sorting and stable summary identity. The executable generated both Markdown and JSON summaries for the committed one-command demonstration transaction.
