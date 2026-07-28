# FutureDiff Task 152 Documentation

## Title

GitHub attestation verification wrapper

## Completed work

Added a fail-closed wrapper around GitHub CLI artifact-attestation verification. It rejects missing subjects, malformed repository identifiers, unavailable tooling, and failed verification while recording only digests of verification output.

## Primary artifacts

scripts/github-attestation-verify.sh

## Safety behavior

The implementation is fail-closed. Missing, stale, malformed, synthetic, unbound, or unverifiable inputs do not produce a production approval.

## Validation

Covered by the cumulative Python unit suite, shell syntax validation, JSON/YAML parsing, package integrity checks, and extracted-package validation.

## Remaining external dependency

Real production certification remains dependent on evidence generated outside this local environment and on merging the overlay into the canonical FutureDiff repository.
