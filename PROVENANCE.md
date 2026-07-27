# Provenance

This package contains:

1. The canonical FutureDiff Go Task 010 implementation.
2. User-provided architecture research and implementation spikes from `futurediff-design(1).zip`.
3. A hardened active `.futurepack` implementation adapted from the supplied artifact-store spike.

The original supplied branch is preserved at `research/original-design-branch/` after removing macOS metadata files. It remains a nested module and is not part of the root module's trusted execution path.

## Tasks 056–060

The v0.60.0 source adds digest-only configuration snapshots, distinct-operator approval quorum enforcement, rotating evidence-encryption keyrings, read-only incident reconstruction, and bounded daemon drain behavior. These additions are part of the canonical Go trusted path and are covered by unit, race, and executable process tests.

## v0.65.0 local assurance additions

The local release includes signed operator receipt verification, policy-driven retention, effect dependency graph export, deterministic SLO evaluation, and a manifest-driven readiness gate. These are local assurance artifacts and do not claim external provider or runtime certification.

## v0.75.0 provenance scope

The release subject set now includes the daemon lock inspector, rate-policy validator, configuration-attestation command, and data-root auditor. Release checksums and in-toto subjects bind all 55 shipped executables plus the SPDX document. Live process validation covered lock exclusion, signed configuration startup, rate rejection, audit-chain verification, configuration drift, and data-root permission failure.

## v0.80.0 local build note

Tasks 076–080 were implemented in the canonical Go repository and validated locally on Linux. The release includes controlled SQLite maintenance, signed integrity checkpoints, expired-lease cleanup, request correlation, and repository admission policy. No external Docker/Podman, GitHub, Slack, agent, macOS, or hosted-attestation certification is claimed for this local build.

## v0.85.0 local build scope

Tasks 081–085 were implemented and validated in the canonical Go tree. The release remains locally generated; hosted GitHub attestation is not claimed. External provider and rootless-container certifications remain separate evidence requirements.
