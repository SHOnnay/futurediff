# FutureDiff master status v0.70.0

## Current position

FutureDiff is a feature-complete local-first transactional effect layer for autonomous agents. It can stage and verify repository changes, prepare and coordinate supported external effects, require cryptographic human approval, publish exact approved results, recover ambiguous outcomes, and produce auditable evidence.

| Target | Complete | Remaining |
|---|---:|---:|
| Architecture and research | 99% | 1% |
| Public open-source MVP | 99.6% | 0.4% |
| Production-grade platform | 81% | 19% |

## Tasks 066–070 completed

- Linux kernel-authenticated Unix peer authorization.
- Durable principal-scoped API idempotency.
- Strict request and response bounds and trailing-JSON rejection.
- Automatic high-confidence secret-scanning verification preflight.
- Resource quotas for transactions, effects, executions, patches, paths, and verification checks.
- Digest-only local mutation API audit.
- Three new operator commands: `futurediff-secret-scan`, `futurediff-quota`, and `futurediff-api-audit`.

## Public MVP work still requiring external infrastructure

- Real rootless Docker and Podman certification.
- Disposable GitHub branch/PR certification.
- Disposable Slack-message certification.
- Full OpenCode and Hermes transaction runs.
- Real token, latency, and compute measurements.
- Native macOS CI execution and release artifacts.
- Hosted GitHub-signed attestation verification.
- Final long-term SQLite driver decision.

## Production work still remaining

- Multi-user identity and RBAC beyond local UID allowlists.
- Short-lived workload identity and managed secret stores.
- Isolated signed third-party adapter processes.
- Distributed coordination and high availability.
- Production database and cloud adapters.
- Encrypted ledger and repository evidence policy.
- Central monitoring, disaster recovery, and operator escalation workflows.
- Independent penetration test and security audit.
- Windows named-pipe and service support.
- Production UI.

## Accurate release description

> Local-first production-oriented MVP with strong deterministic safety controls; external-system certification and distributed enterprise controls remain pending.
