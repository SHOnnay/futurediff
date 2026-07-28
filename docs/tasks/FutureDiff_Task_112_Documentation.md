# FutureDiff Task 112 — Deterministic SLSA-compatible provenance

## Status

Complete.

## Delivered

`provenance-create` generates an in-toto Statement v1 with the SLSA provenance v1 predicate. The subject and byproduct are bound to the canonical source-manifest SHA-256. `provenance-verify` rejects type or digest mismatches.

## Acceptance evidence

Unit tests verify a complete provenance round trip and release-candidate generation records the provenance digest.
