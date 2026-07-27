# ADR-052: Release verification is offline-first

A release must be verifiable without trusting the download location. The verifier checks archive extraction safety, SHA-256 entries, SPDX structure, and in-toto/SLSA subject digests. Signed GitHub attestation verification is an optional additional requirement, not a replacement for offline verification.
