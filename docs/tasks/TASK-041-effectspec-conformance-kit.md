# Task 041 — EffectSpec Conformance Kit

## Goal

Make the public adapter lifecycle testable without relying on provider-specific coordinator code.

## Delivered

- Strict EffectSpec descriptor JSON validation.
- Reusable `effectspec.RunConformance` lifecycle suite.
- Checks for prepared identity, preview digest/fidelity, verification evidence, commit receipts, status reconciliation, abort, and compensation.
- `futurediff-effectspec --self-test` reference implementation.
- Fail-closed behavior: the suite never retries commit after an ambiguous error.

## Limitations

The command does not dynamically load arbitrary Go plugins. Third-party adapter authors import the Go conformance package in their own tests. Executable adapter isolation and signed third-party loading remain production work.
