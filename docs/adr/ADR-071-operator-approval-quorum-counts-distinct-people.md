# ADR-071: Approval quorum counts distinct approvers

## Decision
Quorum approval counts distinct approver identities, not signatures or keys. Duplicate approvers, keys, and nonces are rejected.

## Rationale
Key rotation or multiple keys owned by one person must not satisfy a multi-person control.

## Consequences
The daemon records one quorum approval row with a bundle digest and sorted approver identities. Individual private keys remain outside FutureDiff.
