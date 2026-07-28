# FutureDiff Task 153 Documentation

## Title

Protected promotion workflows

## Completed work

Added manual GitHub workflows for release promotion and production launch. Both use the protected production environment, fixed artifact layouts, cross-run artifact download, policy tests, and retained output evidence.

## Primary artifacts

.github/workflows/release-promotion.yml; .github/workflows/production-launch.yml

## Safety behavior

The implementation is fail-closed. Missing, stale, malformed, synthetic, unbound, or unverifiable inputs do not produce a production approval.

## Validation

Covered by the cumulative Python unit suite, shell syntax validation, JSON/YAML parsing, package integrity checks, and extracted-package validation.

## Remaining external dependency

Real production certification remains dependent on evidence generated outside this local environment and on merging the overlay into the canonical FutureDiff repository.
