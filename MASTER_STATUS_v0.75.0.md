# FutureDiff master status v0.75.0

## Current position

FutureDiff is a production-oriented, local-first transactional effect layer for autonomous agents. The local trust boundary now includes kernel-authenticated clients, exclusive daemon ownership, durable idempotency, quota and rate controls, signed configuration files, secret scanning, cryptographic approvals, tamper-evident transaction events, and tamper-evident API access evidence.

| Target | Complete | Remaining |
|---|---:|---:|
| Architecture and research | 99% | 1% |
| Public open-source MVP | 99.7% | 0.3% |
| Production-grade platform | 83% | 17% |

## Tasks 071–075 completed

- Kernel-enforced exclusive daemon instance lock.
- Inspectable lock-holder metadata.
- Tamper-evident hash chain for payload-free API access events.
- Per-principal read/mutation token buckets and concurrent-mutation limits.
- Detached Ed25519 configuration attestations with expiry.
- Optional daemon enforcement requiring signed security configurations.
- Secure data-root ownership, permission, symlink, and special-file audit.
- Four new operator commands: `futurediff-daemon-lock`, `futurediff-rate-policy`, `futurediff-config-sign`, and `futurediff-root-audit`.

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

- Multi-user RBAC beyond local UID allowlists.
- Short-lived workload identity and managed secret stores.
- Isolated signed third-party adapter processes.
- Distributed coordination and high availability.
- Production database and cloud adapters.
- Ledger/database encryption and enterprise evidence lifecycle policy.
- Central monitoring, disaster recovery, and escalation workflows.
- External audit-log anchoring.
- Independent penetration test and security audit.
- Windows named-pipe/service support and production UI.

## Accurate release description

> Hardened local-first MVP with cryptographic configuration control and single-writer safety; external-system certification and distributed enterprise controls remain pending.
