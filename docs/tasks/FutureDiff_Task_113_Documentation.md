# FutureDiff Task 113 — CycloneDX SBOM generation and verification

## Status

Complete.

## Delivered

- deterministic CycloneDX 1.5 JSON;
- file-level SHA-256 components;
- Go module inventory when `go.mod` is present;
- repository-license metadata;
- extraction-independent verification against current source contents.

## Acceptance evidence

Tests cover successful verification and detection of unexpected files.
