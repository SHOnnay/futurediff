# Task 047 — Deterministic policy bundles

## Objective

Make a verification policy portable, inspectable, and content-addressed without relying on a mutable directory layout.

## Delivered

- `.fdpolicy` deterministic ZIP format.
- Exactly two entries: `manifest.json` and `verification-contract.json`.
- Normalized file order, modes, timestamps, and labels.
- Contract and bundle SHA-256 identities.
- `futurediff-policy-bundle` build and verify modes.
- Strict verification that rejects extra entries, symlinks, oversized entries, malformed contracts, or digest mismatches.
- Policy bundle manifest JSON Schema.

## Validation

Building the same logical policy with differently ordered and duplicated labels produces byte-identical archives. The command builds and independently verifies a bundle from the repository example verification contract.
