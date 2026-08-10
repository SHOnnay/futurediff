# Release Supply-Chain Integrity Certification Report

- **Date**: 2026-08-10T08:10:31Z
- **Host**: Darwin-arm64
- **Nonce**: 20260810-140949-24564
- **Git commit**: 251a1735a8c82fd600298eac72c05b4b6f4d214c
- **Version**: v0.1.0-alpha.3
- **Confirmation**: I_UNDERSTAND_FUTUREDIFF_WILL_CREATE_DISPOSABLE_EVIDENCE_ONLY

## Evidence classes

- **deterministic**: local, byte-verifiable checks that always run (sign/verify
  roundtrip + tamper negative, CycloneDX SBOM create/verify/schema + mutation
  negative, in-toto provenance create/verify + digest match, deterministic
  source-zip double build, packaged build-twice with honest classification,
  checksum sidecar, recomputation of recorded evidence).
- **deterministic_integration**: clean-machine install/upgrade/uninstall drill
  against the published alpha.1 -> alpha.3 assets (network-dependent;
  self-classifies to blocked on network failure and exits non-zero).
- **real**: real local artifacts bound to HEAD (darwin-arm64 packaged release
  signed and verified with the ephemeral keypair, SBOM, provenance), the
  published alpha.3 asset digest-verified and installed via the documented
  installer, and the live read-only `gh attestation verify` attempt.
- **blocked**: hosted/external items with their exact prerequisite, never
  fabricated (external security review, Linux/Windows native clean machines,
  release-hosted signed assets, published SBOM assets, release-signing-key
  custody, byte-identical packaged archives when applicable).

## Results

Deterministic failures: 0 | real failures: 0

See `SUMMARY.json` for the full row-by-row evidence and
`secrets-scan.txt` for the credential scan (0 findings required).
