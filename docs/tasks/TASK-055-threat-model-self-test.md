# Task 055 — Threat-model self-test

## Objective

Turn key local trust assumptions into one executable, machine-readable regression suite.

## Implemented checks

- `TM-001`: approval, commit, recovery, and abort remain outside agent-safe authority.
- `TM-002`: provider egress rejects host and path look-alikes.
- `TM-003`: maintenance mode blocks mutation and detects state forgery.
- `TM-004`: AES-GCM evidence rejects associated-data and ciphertext tampering.
- `TM-005`: signed approvals reject digest tampering and expiry.
- `TM-006`: terminal transaction states have no outgoing transitions.
- `TM-007`: revoked approval keys are rejected and accidental final-key lockout is prevented.

## Command

```bash
futurediff-threat-test --output threat-model-report.json
```

The command exits nonzero if any check fails and binds the report to a SHA-256 digest.

## Limitation

This is a deterministic local regression suite. It is not a penetration test, independent security audit, malware-containment certification, or proof of external provider behavior.

## Validation

All seven controls pass in normal and race-enabled test runs.
