# Task 028 — Offline release verification

## Objective

Allow users and CI to verify a FutureDiff release directory or `.tar.gz` without trusting the hosting site.

## Delivered

- `futurediff-verify-release`
- Safe tar.gz extraction with traversal and special-entry rejection
- `SHA256SUMS` verification
- SPDX 2.3 document validation
- in-toto/SLSA statement validation
- Verification that each provenance subject exists and matches its SHA-256 digest
- Optional `gh attestation verify` enforcement
- JSON verification report

## Executed result

The generated v0.29.0 release directory and archive both passed 21 offline checks. Signed-attestation verification was skipped because no GitHub-signed artifact was available in this environment.
