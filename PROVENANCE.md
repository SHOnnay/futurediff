# Provenance

This package contains:

1. The canonical FutureDiff Go Task 010 implementation.
2. User-provided architecture research and implementation spikes from `futurediff-design(1).zip`.
3. A hardened active `.futurepack` implementation adapted from the supplied artifact-store spike.

The original supplied branch is preserved at `research/original-design-branch/` after removing macOS metadata files. It remains a nested module and is not part of the root module's trusted execution path.

## Tasks 056–060

The v0.60.0 source adds digest-only configuration snapshots, distinct-operator approval quorum enforcement, rotating evidence-encryption keyrings, read-only incident reconstruction, and bounded daemon drain behavior. These additions are part of the canonical Go trusted path and are covered by unit, race, and executable process tests.
