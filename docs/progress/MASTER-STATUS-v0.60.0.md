# FutureDiff Master Status — v0.60.0

## Current position

FutureDiff is a local-first transactional effect layer for autonomous agents. Existing agents propose work; FutureDiff stages repository and provider effects, verifies the exact result, binds approval to the material digest, releases trusted effects, and reconciles ambiguous outcomes without blind retries.

| Target | Complete | Remaining |
|---|---:|---:|
| Architecture and research | 99% | 1% |
| Public open-source MVP | 99.4% | 0.6% |
| Production-grade platform | 78% | 22% |

The accurate description is:

> Feature-complete local MVP with real external-environment certification still pending.

## Completed through Task 060

### Transaction and repository safety
- Durable transaction and effect state machines
- SQLite WAL ledger, migrations, health checks, backup and restore
- Tamper-evident event chains and projection replay
- Git worktree staging, exact patches and exact Git tree identity
- Live checkout protection and create-only FutureDiff refs
- Deterministic verification and content-addressed evidence
- Digest-bound approval and Ed25519 signed approvals
- Distinct-person multi-operator approval quorum

### Execution and credentials
- Rootless OCI execution implementation for Docker and Podman
- Read-only roots, no network by default, capability dropping and resource limits
- Credential broker with operation and destination scopes
- Controlled GitHub and Slack HTTP egress
- AES-256-GCM runtime-evidence encryption
- Rotating evidence keyring with historical decryptors

### External effects
- Durable effect coordinator
- GitHub create-only branch publication
- GitHub draft pull requests
- Slack durable outbox
- Idempotency, provider receipts and ambiguous-result reconciliation

### Agent and operator interfaces
- Local Unix-socket daemon and CLI
- Generic MCP stdio bridge
- OpenCode and Hermes configuration generators
- Installer, systemd user service and launchd user agent
- Maintenance mode and bounded SIGTERM drain
- Digest-only configuration snapshots

### Operations, forensics and release
- Audit, doctor, metrics, pruning and support bundles
- FuturePack transaction exports
- Incident reconstruction reports
- Transaction timelines and diff summaries
- Ledger restoration and upgrade rehearsal
- Policy bundles and policy simulation
- API contract and semantic compatibility tools
- Benchmarks and certification suites
- SBOM, SLSA/in-toto provenance and offline release verification
- 43 Go binaries in v0.60.0

## Tasks 056–060

1. Configuration snapshot attestation
2. Multi-operator approval quorum
3. Evidence-encryption key rotation
4. Incident reconstruction report
5. Bounded daemon drain and shutdown

## Remaining public-MVP work

The remaining 0.6% requires infrastructure unavailable in the local build environment:

- Certify enforced mode on real Docker-rootless and Podman-rootless hosts
- Run disposable GitHub branch and pull-request certification
- Run disposable Slack message certification
- Execute complete OpenCode and Hermes tasks through FutureDiff
- Record real agent token, latency, repair-turn and compute measurements
- Run native macOS CI and publish macOS artifacts
- Publish and verify a GitHub-signed artifact attestation
- Finalize the long-term SQLite driver and distribution decision

## Remaining production work

- Multi-user authentication and RBAC
- Short-lived workload identity and cloud secret managers
- Signed, process-isolated third-party adapters
- Distributed coordination and high availability
- Production database and cloud adapters
- Encrypted retention and organization-level evidence policies
- External event-chain anchoring or transparency log
- Monitoring, disaster recovery and operational SLOs
- Fuzzing campaigns, penetration testing and independent security audit
- Windows named-pipe and service support
- Production UI

## Validation status

- Formatting, vet, unit and race tests: passed
- 43 binaries: built
- Configuration drift detection: passed
- Two-person quorum daemon lifecycle: passed
- Evidence-key historical decryption: passed
- Incident reconstruction: passed
- Live bounded daemon drain: passed
- v0.60.0 offline release verification: 48 pass, 0 fail, 1 skipped

The skipped release check is the hosted GitHub-signed attestation, because the release was generated locally.
