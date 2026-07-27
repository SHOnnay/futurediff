# FutureDiff Architecture

## 1. Design objective

FutureDiff should act as a transaction boundary for autonomous agent tool execution across local and external systems. Its job is not to simulate intelligence; its job is to intercept side effects, stage them, verify them, and either commit them safely or prevent damage.

## 2. Hard requirements

- No second agent run for commit.
- The gateway must be the only holder of real effectful credentials.
- Unsupported tools must fail closed.
- Approval must apply to the exact prepared artifact set, not a recomputed future.
- Every commit path must be idempotent or explicitly marked non-idempotent.
- Crash recovery must be native.
- Irreversible effects must be isolated behind explicit boundaries.

## 3. System decomposition

```text
+----------------------------------------------------------------+
|                        Agent Frameworks                         |
|  OpenCode | Hermes | Claude Code | Codex | OpenHands | Custom   |
+------------------------------+---------------------------------+
                               |
                               | hooks / MCP / proxy / SDK
                               v
+----------------------------------------------------------------+
|                    FutureDiff Control Plane                     |
|----------------------------------------------------------------|
|  Session Gateway        Transaction Coordinator                |
|  Effect Registry        Resource Lock Manager                  |
|  Policy Engine          Approval Service                       |
|  Verification Orchestrator  Reconciliation Worker             |
|  Credential Broker      Audit / Evidence Ledger               |
+--------------------+---------------------------+---------------+
                     |                           |
                     |                           |
                     v                           v
+--------------------------------+   +--------------------------------+
|        Staging Plane           |   |       External Effect Plane     |
|--------------------------------|   |---------------------------------|
|  Worktree Manager              |   | Provider Preview Adapters       |
|  Container Runtime             |   | Durable Outbox Dispatcher       |
|  Disposable Postgres           |   | Compensatable Effect Adapters   |
|  Artifact Builder              |   | Status / Reconcile Connectors   |
+--------------------+-----------+   +-------------------+-------------+
                     |                                   |
                     +----------------+------------------+
                                      v
+----------------------------------------------------------------+
|                  Storage and Evidence Plane                     |
|----------------------------------------------------------------|
|  Postgres metadata + append-only ledger + object/blob storage   |
|  receipt store + redacted/exportable .futurepack artifacts      |
+----------------------------------------------------------------+
                                      |
                                      v
+----------------------------------------------------------------+
|                    Experience / Integration Plane               |
|----------------------------------------------------------------|
| CLI | Dashboard | SDKs | Conformance Tests | Benchmarks         |
+----------------------------------------------------------------+
```

## 4. Core components and responsibilities

### 4.1 Session Gateway
- Starts and tracks agent sessions.
- Intercepts tool calls.
- Normalizes requests into transaction events.
- Rejects direct side-effect calls that bypass registration.

### 4.2 Transaction Coordinator
- Owns transaction state machine.
- Assigns transaction IDs, effect IDs, and idempotency keys.
- Computes safe commit order.
- Decides abort, commit, reconcile, or compensate.

### 4.3 Effect Registry
- Stores adapter metadata and supported lifecycle operations.
- Maps tools to effect classes: R, P, O, C, I.
- Denies execution when no trusted adapter exists.

### 4.4 Resource Lock Manager
- Prevents concurrent transactions from mutating the same resource set.
- Supports advisory locks, leases, and stale-lock recovery.
- Locks on canonical resource URIs, not adapter-specific strings.

### 4.5 Policy Engine
- Evaluates approval rules, domain restrictions, secret handling, egress limits, allowed tools, and commit policies.
- Must version policy bundles so approvals remain tied to a specific rule snapshot.

### 4.6 Verification Orchestrator
- Runs verification contracts in the staged environment.
- Captures evidence: test results, schema diffs, cost estimates, policy decisions, artifact hashes.
- Re-runs lightweight freshness checks before commit.

### 4.7 Approval Service
- Produces approval snapshots from prepared effects and verification evidence.
- Requires re-approval if prepared artifacts or policy versions change.
- Supports human approval and policy-only approval.

### 4.8 Credential Broker
- Holds real credentials centrally.
- Issues scoped, ephemeral, proxy-only credentials to adapters when needed.
- Prevents agents from receiving raw cloud, GitHub, email, or database credentials.

### 4.9 Reconciliation Worker
- Recovers after crashes, timeouts, or ambiguous provider responses.
- Polls `status` on incomplete effects.
- Continues remaining commits, aborts, or compensations according to policy.

### 4.10 Audit / Evidence Ledger
- Append-only event log for tool calls, previews, approvals, commits, aborts, and compensations.
- Tamper-evident by hash-chaining events per transaction.
- Source of truth for recovery and export.

## 5. Effect-class handling rules

### Class R — reversible local
Use worktrees, snapshots, disposable databases, and containers. Commit by applying the exact staged patch or promoted artifact, never by rerunning the agent.

### Class P — provider-previewable
Require provider-generated plan/version IDs where possible. Commit must reference the exact prepared version.

### Class O — outboxable
Prepare exact payloads, store them durably, and release them only after transaction approval and commit ordering rules are satisfied.

### Class C — compensatable
Allow commit only when a compensation strategy is registered, tested, and policy-approved.

### Class I — irreversible
Do not include in a multi-effect automatic commit by default. Force an explicit split boundary, test account, or separate user confirmation.

## 6. Recommended transaction state model

### Transaction states
- `NEW`
- `ACTIVE`
- `VERIFYING`
- `AWAITING_APPROVAL`
- `READY_TO_COMMIT`
- `COMMITTING`
- `COMMITTED`
- `ABORTING`
- `ABORTED`
- `RECONCILING`
- `COMPENSATING`
- `COMPENSATED`
- `FAILED_MANUAL_INTERVENTION`

### Effect states
- `DECLARED`
- `PREPARED`
- `PREVIEWED`
- `VERIFIED`
- `APPROVED`
- `COMMITTING`
- `COMMITTED`
- `ABORTED`
- `COMPENSATING`
- `COMPENSATED`
- `UNKNOWN`

`UNKNOWN` matters. Provider timeouts and crashed processes create ambiguity; the design must represent that directly.

## 7. Commit ordering model

Default order:

1. local staged repository/runtime promotion;
2. provider-previewable effects with version checks;
3. database/schema changes when freshness checks still pass;
4. GitHub/Ticketing artifacts;
5. outbox release such as Slack or email;
6. irreversible effects only in a separate explicit phase.

Reason: notifications should be last, and irreversible actions should not sit in the middle of a saga.

## 8. Visibility model

- Reads inside the same transaction may see staged state.
- Reads outside the transaction must never see uncommitted staged state.
- Cross-transaction reads must not leak prepared effects.
- The dashboard must label prepared, committed, compensated, and unknown effects distinctly.

## 9. Persistence model

### Recommended stores
- **Postgres**: transaction metadata, state machine rows, locks, policy versions, verification summaries.
- **Object storage**: logs, diffs, patch artifacts, DB dumps, provider previews, `.futurepack` bundles.
- **Optional queue**: background work dispatch only; never the source of truth.

### Required persisted entities
- Transaction
- Effect
- Resource lock
- Verification run
- Approval snapshot
- Commit receipt
- Compensation record
- Export manifest
- Credential lease audit record

## 10. Adapter contract

Each trusted adapter should expose:

- `describe`
- `prepare`
- `preview`
- `verify`
- `commit`
- `abort`
- `compensate`
- `status`

The gateway should additionally require adapter metadata for:

- supported resource URI patterns;
- idempotency capability;
- freshness checks;
- compensation availability;
- redaction rules;
- max payload size;
- timeout and retry policy;
- commit-order priority.

## 11. Verification architecture

Verification should have two layers:

### 11.1 Task verification
- tests
- builds
- migration rollback checks
- API contract tests
- dependency/security scans

### 11.2 Transaction verification
- resource versions unchanged since preview
- allowed destination domains only
- no secret leakage in diffs or messages
- external effect count within limits
- approval required for destructive paths
- adapter support level still valid

## 12. Security boundary

This design only has teeth if FutureDiff owns the trust boundary.

Required controls:

- proxy-only provider access;
- agent network egress restrictions;
- sandboxed local execution;
- immutable or append-only ledger records;
- encrypted sensitive artifacts at rest;
- automatic redaction for exports and UI;
- deny-by-default adapter registration.

If the agent can still call GitHub, Slack, or Postgres directly, the product loses its core guarantee.

## 13. Deployment model

### Local developer mode
- single binary or compose stack;
- local Postgres;
- local object store emulator;
- worktree + container runtime;
- one-user approval flow.

### Team / hosted mode
- stateless API services;
- shared Postgres;
- durable blob storage;
- background reconciliation workers;
- SSO-backed approval service;
- tenant-scoped credentials and artifact isolation.

## 14. Refined repository structure

```text
futurediff/
├── specs/
│   ├── effectspec/
│   │   ├── effectspec.schema.json
│   │   ├── lifecycle.md
│   │   └── compatibility-matrix.md
│   └── verification/
│       ├── transaction-events.schema.json
│       └── approval-snapshot.schema.json
├── control-plane/
│   ├── gateway/
│   ├── coordinator/
│   ├── policy/
│   ├── approvals/
│   ├── locks/
│   ├── reconciliation/
│   └── credentials/
├── staging/
│   ├── worktree/
│   ├── runtime/
│   ├── postgres/
│   └── artifacts/
├── adapters/
│   ├── filesystem/
│   ├── git/
│   ├── container/
│   ├── postgres/
│   ├── github/
│   ├── slack/
│   └── shared-testkit/
├── verifier/
│   ├── runners/
│   ├── evidence/
│   ├── policies/
│   └── freshness/
├── sdk/
│   ├── typescript/
│   └── python/
├── integrations/
│   ├── generic-mcp/
│   ├── opencode/
│   ├── hermes/
│   ├── claude-code/
│   └── codex/
├── ui/
│   ├── cli/
│   ├── dashboard/
│   └── exports/
├── benchmarks/
│   ├── baseline-direct/
│   ├── baseline-prompts/
│   ├── baseline-sandbox/
│   ├── cross-tool/
│   ├── crash-recovery/
│   └── adversarial/
└── examples/
    ├── auth-upgrade/
    ├── schema-migration/
    └── github-slack-transaction/
```

## 15. Recommended technology choices

- **Control plane**: Go. Good fit for concurrency, long-lived services, workers, and single-binary distribution.
- **SDKs**: TypeScript and Python.
- **Metadata store**: Postgres.
- **Artifact store**: S3-compatible blob store.
- **Local sandbox runtime**: Git worktrees + container runtime + disposable Postgres.

The exact language can change, but the architectural split should not.

## 16. MVP architecture cut

The first serious MVP should include only:

- Git/filesystem adapter;
- containerized shell/runtime adapter;
- Postgres adapter;
- GitHub adapter;
- Slack adapter;
- CLI inspection flow;
- approval snapshots;
- crash recovery;
- benchmark harness.

Do not build the cinematic spatial UI before the transaction engine, recovery path, and benchmark suite are solid.

## 17. Architecture verdict

The brief is directionally right. The main upgrades needed are:

- make persistence and reconciliation first-class;
- add resource locking and concurrency control;
- separate control plane from staging plane;
- make approval snapshots versioned artifacts;
- isolate irreversible effects more aggressively;
- treat trust-boundary enforcement as mandatory, not optional.
