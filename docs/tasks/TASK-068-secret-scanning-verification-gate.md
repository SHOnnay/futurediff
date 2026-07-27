# Task 068 — Deterministic secret-scanning verification gate

## Goal
Prevent an approved future from publishing high-confidence credentials accidentally added to the repository patch.

## Implemented

- Added-line-only scanning of unified Git patches.
- High-confidence rules for private keys, GitHub tokens, Slack tokens and AWS access keys.
- Medium-confidence generic credential assignment rule.
- SHA-256 fingerprints and short redacted previews; raw secret values are not emitted.
- Versioned `0.1` policy with blocking severities and fingerprint allowlisting.
- Automatic required verification result `futurediff.secret_scan`.
- Blocking findings prevent subsequent verification commands from running.
- Redacted evidence is written under the transaction artifacts directory.
- Standalone `futurediff-secret-scan` command and JSON Schema.

## Limitations

This is deterministic pattern detection, not a complete secret-detection product. It intentionally favors auditable high-confidence rules over opaque ML classification.

## Validation

A GitHub-like token on an added line was blocked, a removed token was ignored, downstream verification was marked blocked, and output contained only a fingerprint and redacted preview.
