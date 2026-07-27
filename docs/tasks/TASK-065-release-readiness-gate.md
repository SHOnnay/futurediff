# Task 065 — Release-readiness gate

## Goal
Combine the strongest local assurance signals into one explicit pass/fail gate.

## Implemented
- Manifest v0.1
- Semantic ledger audit requirement
- SLO policy integration
- API-contract digest pinning
- Maintenance-disabled check
- Optional signed operator-receipt verification
- Deterministic report digest
- `futurediff-readiness` command
- JSON Schema and example

## Scope
The readiness gate certifies local evidence only. It does not replace external Docker, GitHub, Slack, OpenCode, Hermes, macOS, or hosted-attestation certification.
