# FutureDiff Task 144 Documentation

## Title

Temporary risk exceptions

## Completed work

Implemented narrow exception governance with allowlisted scope and risk, detailed rationale, compensating controls, multi-role approval, no owner self-approval, and automatic short expiry.

## Primary artifacts

tools/futurediff_promotion.py; config/risk-exception-policy.json; docs/TRANSPARENCY_AND_EXCEPTIONS.md

## Safety behavior

The implementation is fail-closed. Missing, stale, malformed, synthetic, unbound, or unverifiable inputs do not produce a production approval.

## Validation

Covered by the cumulative Python unit suite, shell syntax validation, JSON/YAML parsing, package integrity checks, and extracted-package validation.

## Remaining external dependency

Real production certification remains dependent on evidence generated outside this local environment and on merging the overlay into the canonical FutureDiff repository.
