# Task 023 — Release Provenance and Attestation

## Delivered

- `futurediff-provenance` command
- in-toto Statement v1 envelope
- SLSA provenance v1 predicate
- SHA-256 subjects for release binaries and SBOM
- Embedded `futurediff.intoto.jsonl` in release packages
- GitHub Actions `actions/attest@v4` release attestation
- Required `id-token`, `attestations`, and `artifact-metadata` workflow permissions

## Important distinction

The locally generated JSONL statement is unsigned descriptive provenance. The GitHub release workflow adds the cryptographically verifiable Sigstore-backed artifact attestation. A release built locally must not be described as signed.
