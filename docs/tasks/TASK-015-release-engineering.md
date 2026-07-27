# Task 015 — Release engineering, build identity, checksums, and SBOM

## Goal

Produce reproducible, self-describing release artifacts instead of distributing arbitrary local binaries.

## Implemented

- Embedded build metadata: version, commit, build date, dirty state, Go version, module, and platform.
- Version output for all FutureDiff commands.
- Eight-command Linux release bundle.
- SPDX 2.3 JSON SBOM generator with per-file SHA-256 hashes.
- Release-level SHA-256 checksum manifests.
- Tagged GitHub release workflow with race tests before publication.
- `scripts/release.sh` and Makefile release targets.
- Architecture, provenance, and README included in release bundles.

## Validation

A local `v0.18.0` Linux x86-64 release archive was built. The embedded version was read back successfully. Its SBOM described 264 files and its checksum manifest contained all eight binaries plus the SBOM.

## Remaining work

Add Sigstore or equivalent signing, SLSA provenance attestation, macOS packaging, and an explicit Windows support decision.
