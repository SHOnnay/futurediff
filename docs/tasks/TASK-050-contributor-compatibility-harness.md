# Task 050 — Contributor compatibility harness

## Objective

Provide one local command that checks whether a contribution remains compatible with FutureDiff's published contracts and safe integration profiles.

## Delivered

- `futurediff-compat`.
- Strict compatibility manifest v0.1 and JSON Schema.
- API baseline comparison against the current daemon contract.
- Verification-contract validation.
- EffectSpec descriptor validation.
- `.fdpolicy` validation.
- Supported configuration linting.
- Relative-path containment and traversal rejection.
- Machine-readable pass/fail report and nonzero failure exit.

## Validation

The repository example manifest passes four compatibility checks. A path escaping the manifest directory is rejected by tests.
