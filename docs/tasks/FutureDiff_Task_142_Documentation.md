# FutureDiff Task 142 Documentation

## Title

Evidence provenance and freshness

## Completed work

Added producer and source allowlists, production-environment enforcement, issued/expiry validation, per-type age limits, future-clock-skew handling, and mandatory rejection of synthetic evidence.

## Primary artifacts

tools/futurediff_promotion.py; config/external-evidence-policy.json

## Safety behavior

The implementation is fail-closed. Missing, stale, malformed, synthetic, unbound, or unverifiable inputs do not produce a production approval.

## Validation

Covered by the cumulative Python unit suite, shell syntax validation, JSON/YAML parsing, package integrity checks, and extracted-package validation.

## Remaining external dependency

Real production certification remains dependent on evidence generated outside this local environment and on merging the overlay into the canonical FutureDiff repository.
