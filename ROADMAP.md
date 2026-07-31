# FutureDiff Roadmap

This roadmap separates the public product from experimental and internal assurance work. It is directional, not a delivery guarantee.

## Public alpha — `v0.1.x`

- local-first guided workflow on Linux and macOS;
- exact diff review and digest-bound approval;
- safe local branch publication;
- optional draft GitHub pull request;
- three-binary release packages with checksums;
- verified installer and shell completions;
- one public end-to-end demonstration;
- clear limitations and security boundaries.

## Public beta — `v0.2.x` and later

- structured JSON logs and stable error codes;
- broader project-aware verification profiles;
- stronger recovery and provider reconciliation UX;
- improved core-path test coverage;
- package-manager distribution such as Homebrew;
- additional real-world interoperability testing;
- independent security review before any 1.0 claim.

## Experimental

These capabilities may exist in source but are not public-alpha guarantees:

- rootless OCI enforced execution;
- Slack effects;
- advanced evidence and provenance pipelines;
- recovery, retention, quota, RBAC, and tenancy controls;
- agent integration profiles;
- operational closure and disaster-recovery tooling.

## Deferred

These require separate product and security designs:

- hosted or network-reachable daemon;
- mTLS or token-based remote authentication;
- multi-user and multi-tenant service operation;
- automatic coding-agent launch and supervision;
- secure Windows daemon and provider runtime;
- formal availability SLOs and enterprise support commitments.

## Versioning

Public product versions follow semantic pre-1.0 versioning. Protocol, API-contract, evidence-schema, and internal task identifiers remain separate and do not determine product maturity.
