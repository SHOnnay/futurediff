# ADR-040: Release artifacts are self-describing

Status: accepted

Every FutureDiff release embeds version, commit, build date, and dirty-state metadata. Tagged Linux releases contain all commands, an SPDX 2.3 SBOM with file hashes, SHA-256 checksums, architecture documentation, and provenance notes. The release workflow runs race tests before publishing.
