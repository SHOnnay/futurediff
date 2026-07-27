# FutureDiff Progress Audit — after Task 009

## Executive assessment

FutureDiff now contains a runnable Go daemon/CLI, durable transaction state, exact Git staging/publication, deterministic verification, rootless-OCI orchestration in source, and a fail-closed credential broker for built-in adapters.

Estimated completion:

| Target | Completion | Change from Task 008 |
|---|---:|---:|
| Narrow open-source MVP | **58%** | +6 percentage points |
| Production-grade platform | **32%** | +5 percentage points |
| Architecture/research design | **91%** | +3 percentage points |

These are weighted engineering estimates based on acceptance criteria and unresolved risk—not line count.

## Component status

| Area | Completion | Current state |
|---|---:|---|
| Go primary implementation | **96%** | Daemon, CLI, packages, tests and binaries build |
| Transaction state model | **86%** | Core and repository recovery states implemented |
| Durable SQLite ledger | **77%** | Five migrations, approvals, evidence and credential audit |
| Git staging/publication | **84%** | Exact patch/tree and FutureDiff ref workflow |
| Deterministic verification | **75%** | DAG, OCI checks and durable READY gate |
| OCI source implementation | **70%** | Integrated and fake-runtime tested |
| Real rootless certification | **15%** | Script exists; Docker/Podman unavailable here |
| Local daemon/API/CLI | **79%** | Unix-socket lifecycle and health status |
| Recovery/reconciliation | **59%** | Repository recovery; provider recovery pending |
| Credential broker | **62%** | Scoped built-in access, audit, redaction, env source |
| Adapter trust boundary | **42%** | Identity registry and denial policy; process isolation pending |
| EffectSpec | **37%** | Interface and tests; conformance runner pending |
| Controlled network egress | **12%** | Network-off only; allow-listed proxy absent |
| External-effect coordinator | **15%** | Schema foundations only |
| GitHub adapter | **35%** | Experimental spike exists outside canonical path |
| Slack outbox | **30%** | Experimental spike exists outside canonical path |
| PostgreSQL adapter | **25%** | Preview spike exists outside canonical path |
| OpenCode/Hermes/MCP integrations | **5%** | Not yet canonical |
| Packaging/release/signing | **20%** | Local builds/checksums; no signed release pipeline |
| Cross-platform support | **20%** | Unix path; Windows named pipe/ACL absent |
| UI | **0%** | Intentionally deferred |

## What Task 009 completed

- strict credential config and schema;
- environment secret source for bootstrap;
- no raw secret API;
- no secret or plaintext source reference in SQLite;
- built-in adapter identity enforcement;
- verified/untrusted denial;
- exact operation and HTTPS destination scoping;
- expiry and enabled checks;
- durable audit before secret release;
- fail-closed audit/source errors;
- redacted secret formatting and adapter errors;
- minimal environment for Git and OCI probe subprocesses;
- daemon health integration;
- migration 0005 and tests.

## Remaining to first public MVP — approximately 42%

1. **Real rootless certification — 4% of MVP**  
   Run Docker-rootless and Podman-rootless certification on Linux and publish evidence.

2. **External-effect coordinator — 9%**  
   Durable prepared effects, dependency DAG, write-ahead commit intent, receipts, unknown status, compensation.

3. **Canonical GitHub adapter — 7%**  
   Draft PR prepare/preview/verify/commit/status through the broker.

4. **Slack durable outbox — 5%**  
   Exact message preview, commit last, receipt and deletion compensation where supported.

5. **Controlled egress — 5%**  
   Provider allow-list proxy and DNS/IP rebinding protections.

6. **Agent integration — 5%**  
   Generic MCP proxy or OpenCode integration, then Hermes.

7. **Benchmark and adversarial suite — 4%**  
   Compare direct, permission-only, sandbox-only, and FutureDiff.

8. **Packaging and release — 3%**  
   Installer, dependency review, SBOM, signed checksums, release CI.

## Remaining for production-grade platform — approximately 68%

Beyond the public MVP, production readiness requires:

- OS keyring, Vault, cloud secret manager, and short-lived identity support;
- isolated signed third-party adapter processes;
- multi-user authentication and authorization;
- controlled read and effect network channels;
- provider-specific idempotency and reconciliation;
- PostgreSQL/cloud/database safety adapters;
- Windows support;
- distributed coordinator leases and server mode;
- security review, fuzzing, penetration tests, and malicious-container tests;
- observability, retention, backup, upgrade, and operational tooling;
- stable protocol/version compatibility policy.

## Recommended next task

**Task 010: Durable External-Effect Coordinator plus the first canonical GitHub draft-PR adapter.**
