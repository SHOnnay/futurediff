# FutureDiff Task 143 Documentation

## Title

Hosted identity claims policy

## Completed work

Added policy validation for issuer, audience, repository, workflow reference, protected Git ref, event, actor, run identifier, source SHA, and token time window. This validates claims supplied by a trusted token or attestation verifier; it does not treat self-authored claims as cryptographic proof.

## Primary artifacts

tools/futurediff_promotion.py; config/hosted-identity-policy.json; examples/hosted-oidc-claims.example.json

## Safety behavior

The implementation is fail-closed. Missing, stale, malformed, synthetic, unbound, or unverifiable inputs do not produce a production approval.

## Validation

Covered by the cumulative Python unit suite, shell syntax validation, JSON/YAML parsing, package integrity checks, and extracted-package validation.

## Remaining external dependency

Real production certification remains dependent on evidence generated outside this local environment and on merging the overlay into the canonical FutureDiff repository.
