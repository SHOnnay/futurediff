# ADR-070: Configuration attestation is digest-only

## Decision
FutureDiff configuration snapshots store canonical file paths, existence, permissions, sizes, and SHA-256 digests. They never copy configuration contents.

## Rationale
The snapshot must detect drift without duplicating credential metadata or secret-bearing files into a second artifact.

## Consequences
A snapshot proves file identity and metadata, not semantic equivalence. Any byte change requires a new snapshot.
