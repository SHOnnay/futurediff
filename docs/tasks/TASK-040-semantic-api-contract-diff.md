# Task 040 — Semantic API Contract Diff

## Goal

Detect local-daemon API incompatibility semantically rather than comparing only one opaque digest.

## Implemented

- `futurediff-api-diff --baseline <contract.json>`.
- Candidate from current client, a file, or a running daemon socket.
- Contract structural validation.
- Canonical digest verification.
- Duplicate endpoint and operation-ID rejection.
- Breaking endpoint-removal detection.
- Operation-ID change detection.
- Agent-safety change detection as incompatible.
- Additive endpoint reporting without marking an otherwise compatible contract as broken.
- Major-version compatibility check.

## Validation

The checked-in v1 contract is compatible with the current client. Removed endpoints, digest mismatches, duplicate endpoints, and agent-authority changes are rejected by tests.
