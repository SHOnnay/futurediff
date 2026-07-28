# FutureDiff master status — v0.95.0

## Current characterization

**Feature-complete local MVP with kernel-authenticated RBAC and transaction-level resource isolation; external certification and distributed production infrastructure remain pending.**

FutureDiff is a Go transactional effect layer that stages autonomous-agent changes as reviewable futures, verifies exact material, binds operator approval, and releases persistent effects through durable and recoverable workflows.

## Completed through Task 095

In addition to the capabilities completed through Task 090, v0.95.0 adds:

- durable owner identity on every newly created transaction;
- `owned` and `all` resource scopes on local UID roles;
- controlled `read` and `operate` transaction sharing;
- explicit prohibition on delegated transaction administration;
- principal-scoped transaction listing at SQL query time;
- not-found privacy for inaccessible transaction identifiers;
- immediate access revocation;
- an independent tamper-evident transaction-access event chain;
- transaction-access chain coverage in semantic audits and signed integrity checkpoints;
- a 13-control tenant-isolation conformance suite;
- a real Linux multi-UID daemon validation using kernel peer identities.

The release contains **70 Go commands** and exposes local API contract version **1.1**.

## Current completion

| Target | Complete | Remaining |
|---|---:|---:|
| Architecture and research | 99% | 1% |
| Public open-source MVP | 99.85% | 0.15% |
| Production-grade platform | 91% | 9% |

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

- remote identity, enterprise SSO, organization and group synchronization;
- network transport security beyond the local Unix socket;
- short-lived workload identity and external secret managers;
- isolated and signed third-party adapter execution;
- distributed coordination, high availability and PostgreSQL/cloud persistence;
- encrypted evidence-retention governance and formal key custody;
- monitoring, disaster recovery, penetration testing and independent security assessment;
- Windows named-pipe/service support;
- production user interface.

## Important claims boundary

- Transaction isolation is local Linux principal isolation, not enterprise multi-tenant IAM.
- `operate` shares cover agent-safe mutations only; they do not confer approval, commit, recovery, abort, or access-administration authority.
- Signed capabilities can delegate one exact unsafe operation even when ownership would otherwise deny it; they remain short-lived and single-use.
- Access, authorization, API, and transaction chains are locally tamper-evident, not externally timestamped transparency logs.
- No real Docker, Podman, GitHub, Slack, OpenCode, Hermes, macOS or hosted-attestation certification is claimed by this local release.
