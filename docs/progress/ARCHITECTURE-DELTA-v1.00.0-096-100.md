# FutureDiff Architecture v10.0 — Tasks 096–100 delta

## Time-bounded sharing plane

Transaction shares may carry a bounded `expires_at` timestamp. Authorization and principal-scoped discovery evaluate expiry synchronously, so delegated authority ends without relying on a scheduler. Stored rows remain available for inspection until offline cleanup.

## Access-retention plane

Expired share deletion is a separate maintenance operation. The daemon must be stopped, the exclusive data-root lock must be held, and a digest-bound plan must survive exact candidate revalidation. Each removed row creates a revocation event linked to the cleanup plan digest.

## Principal-fairness plane

Tenant quotas constrain open transactions per owner, active shares per transaction, and active shared transactions per recipient. These controls supplement the global resource quotas; neither policy silently overrides the other.

## Privacy-minimized tenancy-observation plane

Tenant inventory reports operational counts and status distributions while replacing local principal identities with SHA-256 values by default. Repository identity, code material, provider payloads, request bodies and credentials are outside this projection.

## Governance-conformance plane

A deterministic 15-control suite validates expiry, cleanup, principal fairness and redacted inventory. The suite is local release evidence and is not represented as enterprise IAM or distributed multi-tenant certification.
