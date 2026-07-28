# FutureDiff master status — v0.90.0

## Current characterization

**Feature-complete local MVP with deny-by-default multi-user authorization; external certification and distributed production infrastructure remain pending.**

FutureDiff is a Go transactional effect layer that stages autonomous-agent changes as reviewable futures, verifies exact material, binds operator approval, and releases persistent effects through durable and recoverable workflows.

## Completed through Task 090

In addition to the capabilities completed through Task 085, v0.90.0 adds:

- UID-based local role-based access control over Linux kernel peer credentials;
- canonical API operation matching and deny-by-default policy compilation;
- an independent tamper-evident authorization decision chain;
- short-lived, one-time Ed25519 capabilities scoped to UID, operation and transaction;
- offline authorization policy explanation and simulation;
- a 12-check access-control conformance suite;
- authorization-chain coverage in semantic ledger audits and signed integrity checkpoints.

The release contains **68 Go commands**.

## Current completion

| Target | Complete | Remaining |
|---|---:|---:|
| Architecture and research | 99% | 1% |
| Public open-source MVP | 99.85% | 0.15% |
| Production-grade platform | 89% | 11% |

## Remaining public-MVP evidence

- real Docker-rootless and Podman-rootless certification;
- disposable GitHub branch/PR certification;
- disposable Slack exactly-once certification;
- complete live OpenCode and Hermes transactions;
- actual token, latency and compute measurements;
- native macOS CI execution and artifacts;
- hosted GitHub-signed attestation verification;
- final long-term SQLite driver decision.

## Remaining production work

- remote identity, enterprise SSO and organization-level IAM integration;
- network transport security beyond the local Unix socket;
- short-lived workload identity and external secret managers;
- isolated and signed third-party adapter execution;
- distributed coordination, high availability and PostgreSQL/cloud persistence;
- encrypted evidence-retention governance and formal key custody;
- monitoring, disaster recovery, penetration testing and independent security assessment;
- Windows named-pipe/service support;
- production user interface.

## Important claims boundary

- UID RBAC is local Linux authorization, not enterprise IAM.
- Signed capabilities are one-time delegated local authority, not bearer credentials for remote use.
- Authorization and API chains are locally tamper-evident, not externally timestamped transparency logs.
- No real Docker, Podman, GitHub, Slack, OpenCode, Hermes, macOS or hosted-attestation certification is claimed by this local release.
