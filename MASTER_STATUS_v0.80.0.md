# FutureDiff master status — v0.80.0

## Current characterization

**Feature-complete local MVP with external certification still pending.**

FutureDiff is a Go-based transactional effect layer that stages autonomous-agent changes as reviewable futures, verifies them, binds human approval to exact material, and releases persistent effects through durable, recoverable workflows.

## Completed through Task 080

The implementation includes:

- private Unix-socket daemon with kernel peer authorization;
- durable SQLite ledger, migrations, backup, restore, replay and upgrade rehearsal;
- exclusive daemon locking, secure-root audit and bounded draining;
- detached Git worktrees, exact patch identity and live-checkout protection;
- rootless OCI enforcement architecture and certification tooling;
- deterministic verification and high-confidence secret scanning;
- credential broker and controlled provider egress;
- GitHub branch/draft-PR and Slack outbox effect coordinators;
- durable idempotency, quotas, rate limits and payload-free API auditing;
- Ed25519 signed approvals, quorum policies, key rotation and signed configuration;
- encrypted runtime evidence and evidence-key rotation;
- forensic FuturePack, incident, timeline, diff and dependency-graph exports;
- maintenance, pruning, retention, metrics, SLO and readiness controls;
- controlled SQLite maintenance and expired-lease cleanup;
- signed integrity checkpoints;
- request correlation bound to the API audit chain;
- canonical-root repository admission policy;
- release SBOM, SLSA/in-toto provenance and offline verification.

## Current completion

| Target | Complete | Remaining |
|---|---:|---:|
| Architecture and research | 99% | 1% |
| Public open-source MVP | 99.8% | 0.2% |
| Production-grade platform | 85% | 15% |

## Remaining public-MVP work

- real Docker-rootless and Podman-rootless certification;
- disposable GitHub branch/PR certification;
- disposable Slack exactly-once certification;
- complete live OpenCode and Hermes transactions;
- actual token, latency and compute measurements;
- native macOS CI execution and artifacts;
- hosted GitHub-signed attestation verification;
- final long-term SQLite driver decision.

## Remaining production work

- multi-user authentication and RBAC;
- short-lived workload identity and external secret managers;
- isolated, signed third-party adapter execution;
- distributed coordination and high availability;
- production PostgreSQL/cloud adapters;
- encrypted retention and evidence lifecycle governance;
- monitoring, disaster recovery and independent security assessment;
- Windows named-pipe/service support;
- production user interface.
