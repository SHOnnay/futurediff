# FutureDiff Progress Audit — After Task 013

**Date:** 2026-07-27  
**Method:** Weighted acceptance criteria and unresolved risk, not line count

## 1. Overall completion

| Target | Complete | Remaining |
|---|---:|---:|
| Architecture and research | 98% | 2% |
| Narrow public open-source MVP | 78% | 22% |
| Production-grade platform | 46% | 54% |

## 2. Component progress

| Component | Completion |
|---|---:|
| Go primary implementation | 98% |
| Transaction state model | 92% |
| Git staging and local publication | 89% |
| Local daemon, API and CLI | 87% |
| Durable SQLite ledger | 85% |
| Deterministic verification | 80% |
| Recovery and reconciliation | 76% |
| GitHub draft-PR adapter | 75% |
| Controlled GitHub branch publication | 72% |
| Slack durable outbox | 70% |
| External-effect coordinator | 68% |
| Credential broker | 67% |
| Generic MCP integration | 62% |
| EffectSpec | 52% |
| Adapter trust boundary | 48% |
| OCI source implementation | 70% |
| Real rootless host certification | 15% |
| Controlled provider/agent egress | 12% |
| PostgreSQL production adapter | 25% experimental |
| Packaging and release | 27% |
| Dedicated OpenCode/Hermes adapters | 10% |
| UI | 0% intentionally |

## 3. Narrow MVP work remaining — 22%

### A. Real rootless OCI certification and adversarial isolation — 5%

- certify Docker rootless;
- certify Podman rootless;
- test live-checkout protection on real runtimes;
- test credential absence and network denial;
- test timeout, cancellation, PID/memory/disk controls;
- run malicious workspace escape scenarios.

### B. Release engineering — 4%

- reproducible release workflow;
- install script/package manager path;
- SBOM;
- signed checksums and provenance;
- version command;
- migration and downgrade/rollback policy;
- supported-platform matrix.

### C. Reproducible benchmark and adversarial suite — 4%

Compare:

```text
direct execution
permission prompts
sandbox only
FutureDiff
```

Measure task completion, duplicate effects, released failures, human approvals, recovery, token overhead, latency, and compute overhead.

### D. Real provider certification — 3%

- dedicated GitHub test repository;
- create-only branch publication;
- draft PR creation and ambiguous recovery drills;
- dedicated Slack test workspace;
- outbox post and reconciliation drills;
- verify provider rate-limit and pagination behavior.

### E. One-command demo and MCP host configurations — 3%

- scripted demo repository;
- deterministic no-key playback;
- tested client configurations;
- clear trusted approval handoff;
- launch-quality README video/script.

### F. SQLite and migration hardening — 2%

- replace or formally accept the bootstrap cgo bridge;
- migration upgrade tests from every released schema;
- backup/restore test;
- corruption and disk-full behavior;
- increase ledger test coverage.

### G. CI/platform coverage — 1%

- macOS build and socket smoke;
- Windows design/build or documented exclusion;
- multiple supported Git/SQLite versions.

## 4. Production work remaining — 54%

The production target additionally requires:

- OS keychain, Vault, cloud secret manager, and workload identity support;
- short-lived provider credentials;
- signed and isolated third-party adapter processes;
- controlled allow-listed egress with DNS/IP defenses;
- production PostgreSQL and cloud adapters;
- compensating operations and operator workflows;
- multi-user authentication, RBAC, and approval identities;
- distributed coordinator/server mode;
- encrypted evidence and retention policy;
- monitoring, backup, upgrade, and disaster recovery;
- fuzzing, penetration testing, and independent security audit;
- stable protocol/version compatibility policy;
- Windows named-pipe support if Windows is included.

## 5. What the three new steps changed

Task 011 closed the integrity gap between an approved local tree and the pull request's remote head.

Task 012 added a second heterogeneous provider effect and demonstrated late, dependency-aware, duplicate-resistant message release.

Task 013 created the first generic agent integration while preserving the central rule that the agent can prepare a future but cannot approve or commit it.

The project now has a coherent narrow end-to-end product path. Most remaining MVP work is certification, distribution, benchmark evidence, and real-provider validation rather than another architectural rewrite.
