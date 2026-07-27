# FutureDiff Progress Audit — After Task 010

**Date:** 2026-07-27  
**Method:** Weighted acceptance criteria and residual risk, not source lines or file counts

## 1. Overall completion

| Target | Completed | Remaining |
|---|---:|---:|
| Architecture and research | **94%** | 6% |
| Narrow open-source MVP | **65%** | 35% |
| Production-grade platform | **38%** | 62% |

## 2. Component completion

| Component | Completion | Notes |
|---|---:|---|
| Go primary implementation | **97%** | Go is authoritative; no Rust/Node primary path |
| Transaction state model | **90%** | External completion and reconciliation integrated |
| Git staging and local publication | **85%** | Safe local ref complete; remote publication missing |
| Local daemon, API, and CLI | **83%** | External-effect API added; auth/multi-user not present |
| Durable SQLite ledger | **83%** | Effect documents, attempts, receipts, leases added |
| Deterministic verification | **79%** | Effects included in material; more policy checks remain |
| OCI source implementation | **70%** | Real rootless host certification pending |
| Credential broker | **66%** | Environment source only; no keychain/workload identity |
| Recovery and reconciliation | **69%** | Git and first GitHub unknown outcome supported |
| External-effect coordinator | **52%** | First effect works; dependencies/compensation remain |
| GitHub draft-PR adapter | **64%** | Canonical draft creation; remote branch publication absent |
| Adapter trust boundary | **46%** | Built-ins only; process isolation/signing absent |
| EffectSpec | **49%** | Lifecycle used by first effect but not complete ecosystem |
| Controlled network egress | **12%** | OCI remains network-none; provider plane is daemon-side |
| Slack durable outbox | **30% experimental** | Exists only in uploaded spike branch |
| PostgreSQL adapter | **25% experimental** | Preview/locking spike, not canonical |
| OpenCode/Hermes/MCP integrations | **5%** | Not canonical yet |
| Packaging and release | **22%** | Builds/checksums available; installer/signing/SBOM absent |
| UI | **0% intentionally** | Deferred until infrastructure is credible |

## 3. What Task 010 closed

Task 010 closed these important gaps:

- external effects are durable transaction material rather than ad hoc API calls;
- provider mutation is fenced by a coordinator lease;
- commit intent is recorded before mutation;
- operation-specific credentials are auditable;
- provider resource versions invalidate stale approval;
- transport ambiguity is represented explicitly;
- recovery avoids a blind duplicate PR creation;
- local repository success alone cannot finalize a transaction with required external effects.

## 4. Remaining work for narrow MVP — 35%

### Task 011: remote branch publication and dependencies — approximately 7%

Publish the exact approved local commit/tree to a controlled GitHub branch and make draft-PR creation depend on its receipt.

### Slack durable outbox — approximately 5%

Prepare exact channel/message payload, bind it to approval, and release it last with status reconciliation.

### Generic MCP proxy and one agent integration — approximately 6%

At minimum:

- generic MCP effect gateway;
- OpenCode or Hermes integration;
- no provider credentials in agent environment.

### Controlled provider egress — approximately 4%

Separate provider adapter traffic from agent network access, with allow-listed destinations and DNS/IP controls.

### Rootless OCI certification — approximately 4%

Run Docker-rootless and/or Podman-rootless tests on a real Linux host, including attempted credential, mount, and network escapes.

### Benchmark and adversarial suite — approximately 4%

Compare direct execution, permission prompts, sandbox-only, and FutureDiff for duplicate effects, stale state, partial failure, and human review burden.

### Packaging and release engineering — approximately 3%

Installer, SBOM, reproducible release procedure, signed checksums, upgrade policy, and security documentation.

### Test-depth and documentation polish — approximately 2%

Raise ledger/API coverage, fuzz parsers and state transitions, and publish a contributor adapter guide.

## 5. Remaining work for production platform — 62%

Production readiness additionally requires:

- OS keychain and external secret-manager integrations;
- short-lived provider credentials and workload identity;
- signed, isolated third-party adapter processes;
- multi-user authentication and authorization;
- distributed coordination and durable server deployment;
- provider-specific compensation and reconciliation;
- hardened egress proxy and DNS-rebinding protection;
- Windows support;
- data retention, backup, and migration tooling;
- security audit, penetration test, fuzzing, and malicious-container suite;
- observability and operational alerting;
- stable protocol compatibility and deprecation policy;
- production GitHub, Slack, PostgreSQL, and cloud adapters.

## 6. Highest-risk unresolved issue

The most important functional gap is the missing causal binding between the approved local repository result and the remote GitHub head branch.

Current Task 010 behavior:

```text
approved local FutureDiff ref
        ≠ automatically published remote head
        ↓
draft PR uses an independently existing remote head
```

Required Task 011 behavior:

```text
approved local result
        ↓
controlled remote branch publication receipt
        ↓
verified remote SHA
        ↓
draft PR dependency released
```

Until Task 011 is complete, the GitHub draft-PR adapter is useful for transaction research but not yet the final repository-to-PR workflow.
