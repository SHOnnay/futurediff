# FutureDiff Task 141 Documentation

## Title

External evidence intake

## Completed work

Implemented a fail-closed intake engine for externally produced certification files. The evaluator rejects missing files, symbolic links, unsafe paths, duplicate identifiers, missing evidence types, and mismatched SHA-256 declarations.

## Primary artifacts

tools/futurediff_promotion.py; config/external-evidence-policy.json; schemas/external-evidence-specification.schema.json

## Safety behavior

The implementation is fail-closed. Missing, stale, malformed, synthetic, unbound, or unverifiable inputs do not produce a production approval.

## Validation

Covered by the cumulative Python unit suite, shell syntax validation, JSON/YAML parsing, package integrity checks, and extracted-package validation.

## Remaining external dependency

Real production certification remains dependent on evidence generated outside this local environment and on merging the overlay into the canonical FutureDiff repository.
