# FutureDiff System Architecture v6.0

**Status:** Canonical Go architecture after Tasks 056–060
**Date:** 2026-07-27  
**Scope:** Transactional effects, multi-operator approvals, rotating encrypted evidence, incident reconstruction, and bounded daemon lifecycle
**UI:** Intentionally deferred; future visual direction remains black, matte black, graphite, and gray

## 1. Mission

FutureDiff is a transactional effect layer for existing autonomous agents.

```text
agent decides
    → FutureDiff stages local and external effects
    → deterministic verification produces evidence
    → approval binds to the exact transaction material
    → trusted adapters release effects through scoped credentials
    → ambiguous outcomes reconcile instead of blindly retrying
```

FutureDiff is not another agent, an observability dashboard, a normal Docker wrapper, or a universal distributed ACID system.

The core architectural promise is narrower and testable:

> Existing agents may propose and stage consequences, but only FutureDiff's trusted transaction path may approve and release persistent effects.

## 2. Current end-to-end architecture

```text
MCP-capable agent / Go CLI / future native adapter
                    ↓
          unprivileged staging interface
                    ↓
   private HTTP over Unix socket (mode 0600)
                    ↓
            Go daemon and coordinator
                    ↓
       SQLite WAL ledger + durable events
                    ↓
  ┌─────────────────┴──────────────────┐
  │                                    │
  ▼                                    ▼
Git detached worktree          Prepared provider effects
  │                           ├── GitHub branch publication
  │                           ├── GitHub draft pull request
  │                           └── Slack durable outbox
  ▼                                    │
rootless OCI execution                 │
when enforced-ready                    │
  │                                    │
  └───────────────┬────────────────────┘
                  ▼
       deterministic verification
                  ↓
    dependency-aware effect commit DAG
                  ↓
 combined repository + provider digest
                  ↓
        trusted human/policy approval
                  ↓
     coordinator lease + fencing token
                  ↓
   exact local Git ref materialization
                  ↓
 create-only remote FutureDiff branch
                  ↓
      dependent draft pull request
                  ↓
       dependent Slack notification
                  ↓
     receipts or explicit reconciliation
```

## 3. Operating modes

### Cooperative mode

- durable Git staging and deterministic verification;
- caller may edit the detached transaction workspace directly;
- provider effects still use the credential broker and built-in adapters;
- alternate host-side mutation paths are not claimed to be prevented.

### Enforced mode

- requires a digest-pinned rootless Docker or Podman runtime;
- agent-authored commands run in a sanitized OCI workspace;
- no provider credentials or inherited host environment;
- network disabled by default;
- live checkout and Git metadata are not mounted;
- workspace synchronization is evidence-gated;
- real rootless-host certification remains pending on a suitable Linux host.

## 4. Trust and authority boundaries

### Agent authority

An agent may:

- create a transaction;
- run commands in an enforced staged workspace;
- seal staged changes;
- request deterministic verification;
- prepare external effects;
- inspect transaction and effect status.

An agent may not, through the generic MCP bridge:

- approve a transaction;
- commit a transaction;
- directly receive provider credentials;
- bypass the effect coordinator;
- override verification results.

### Trusted release authority

The local CLI/API approval path remains outside the MCP tool surface. Release requires:

1. a current transaction digest;
2. durable approval bound to that digest;
3. a valid coordinator lease and fencing token;
4. fresh resource versions;
5. satisfied effect dependencies;
6. write-ahead provider intent;
7. exact provider-operation credential grant.

## 5. Durable ledger

SQLite migrations track:

- transactions, state revisions, material revisions, approvals, and events;
- workspaces, patches, exact Git trees, and materialized local refs;
- OCI executions and verification evidence;
- external effects, effect dependencies, prepared documents, attempts, receipts, and reconciliation state;
- adapter identities, credential metadata, and credential-access decisions.

Secret values are never persisted.

The current local-first implementation uses SQLite in WAL mode. It does not yet provide multi-node distributed coordination.

## 6. Repository staging and deterministic commit identity

FutureDiff pins the repository source revision and creates a detached transaction worktree. Sealing captures:

```text
binary-capable patch
patch SHA-256
exact staged Git tree
base commit
source reference
material revision
```

Task 011 adds deterministic commit prediction before publication:

```text
commit = git commit-tree(
    staged_tree,
    parent = base_commit,
    fixed FutureDiff author/committer,
    timestamp = patch generation time,
    deterministic message
)
```

The predicted commit object is created without publishing a ref. This lets external effects bind to the exact commit identity that will later be materialized after approval.

The user’s current checkout, branch, index, and `HEAD` remain unchanged.

## 7. Effect dependency DAG

Every external effect may declare `depends_on` effect IDs.

The coordinator enforces:

- referenced effects belong to the same transaction;
- dependencies exist before the dependent effect is recorded;
- dependency state is included in verification and approval material;
- a dependent effect cannot commit before all dependencies have durable receipts;
- failed or unresolved dependencies prevent release;
- dependency ordering is deterministic.

The current canonical chain is:

```text
GitHub branch publication
        ↓
GitHub draft pull request
        ↓
Slack notification
```

This is orchestration, not a global provider transaction. Each provider effect retains its own outcome and reconciliation state.

## 8. Controlled GitHub branch publication

### Purpose

Publish the exact approved FutureDiff commit to a new remote branch before creating a dependent pull request.

### Restrictions

- branch must be under `futurediff/*`;
- remote URL must be credential-free HTTPS;
- owner/repository path must match exactly;
- publication is create-only;
- an existing branch is rejected;
- no force-update of an existing ref;
- no deletion, merge, tag, or release operation.

### Credential transport

The GitHub token is not placed in:

- command arguments;
- remote URL;
- subprocess environment;
- SQLite;
- durable events.

The secure Git runner passes the token through an inherited file descriptor to a temporary `GIT_ASKPASS` helper. Git is invoked with sanitized environment and redirect-disabled HTTP configuration.

### Create-only safety

The adapter uses an absent-ref lease equivalent:

```text
--force-with-lease=refs/heads/<branch>:
```

The push may create the branch only while the expected old value is absent.

### Recovery

After any ambiguous push result, FutureDiff queries the exact remote branch:

- exact approved commit observed → persist recovered receipt;
- branch absent → safe to re-arm according to coordinator policy;
- different commit observed → conflict/manual resolution;
- status itself unknown → remain in reconciliation.

## 9. GitHub draft pull request binding

A draft pull request can depend on the branch-publication effect.

Before branch publication, pre-commit verification checks:

- exact base SHA remains fresh;
- dependent predicted head commit is known;
- remote head may be absent or may already equal the predicted commit.

After branch publication, final freshness verification requires the remote head to equal the approved predicted commit exactly.

The PR effect includes a unique marker:

```html
<!-- futurediff-effect:<effect-id> -->
```

This supports duplicate avoidance and ambiguous-result reconciliation.

## 10. Slack durable outbox

A Slack message is prepared as a durable outbox effect. Preparation stores:

- channel ID;
- exact message text;
- stable `client_msg_id` derived from the effect identity;
- FutureDiff metadata marker;
- request digest;
- dependencies;
- destination and operation scope.

The message is not sent during preparation, verification, or approval.

Release occurs only when:

1. the transaction digest is approved;
2. all declared dependencies have committed receipts;
3. a write-ahead attempt exists;
4. the Slack credential broker grants the exact operation and destination;
5. status-before-post does not find an existing matching message.

If Slack accepts the message but the connection fails before the response arrives, the effect becomes `UNKNOWN`. Recovery searches channel history for the exact `client_msg_id` or FutureDiff metadata marker. It does not blindly post again.

Social consequences are not described as reversible merely because an API may allow deletion.

## 11. Credential broker

The credential broker releases secret material only inside reviewed built-in adapter callbacks after validating:

1. adapter identity and built-in trust;
2. credential binding;
3. exact operation;
4. exact canonical HTTPS destination;
5. expiry and enabled state;
6. durable audit write.

Current exact operations include:

```text
github.query_git_ref
github.publish_branch
github.read_refs
github.query_pull_requests
github.create_draft_pull_request
slack.query_channel_history
slack.post_message
```

Verified and untrusted third-party adapters remain credential-ineligible until executable verification and process isolation are implemented.

## 12. Generic MCP stdio bridge

Task 013 introduces `futurediff-mcp`, a zero-dependency Go stdio bridge that translates MCP tool calls to the private FutureDiff daemon API.

Properties:

- newline-delimited UTF-8 JSON-RPC over stdin/stdout;
- protocol version `2025-11-25`;
- initialization handshake required;
- maximum inbound message size of 8 MiB;
- protocol output only on stdout;
- operational errors returned as tool results;
- no approval or commit tools;
- daemon remains the single transaction authority.

Exposed tools:

```text
futurediff.transaction_create
futurediff.transaction_status
futurediff.transaction_execute
futurediff.transaction_seal
futurediff.transaction_verify
futurediff.effects_list
futurediff.github_branch_prepare
futurediff.github_pr_prepare
futurediff.slack_message_prepare
```

The bridge is intentionally unprivileged. An MCP-connected model can construct and verify a future but cannot release it.

## 13. Transaction and effect outcomes

A provider timeout is not treated as failure.

```text
COMMITTING
    → UNKNOWN
    → status reconciliation
        ├── COMMITTED
        ├── VERIFIED / safe to re-arm
        ├── CONFLICT
        └── MANUAL_INTERVENTION
```

Final transaction completion requires:

- exact local repository materialization;
- every required external effect has a durable receipt;
- every dependency is satisfied;
- receipt request digest matches prepared material;
- coordinator fencing token is valid;
- approval digest still matches the current material revision.

## 14. Current security posture

Implemented in source and tests:

- live-checkout protection;
- deterministic approval material;
- rootless OCI command construction;
- credential isolation;
- sanitized subprocess environments;
- scoped built-in adapters;
- write-ahead provider effects;
- create-only Git publication;
- duplicate-resistant GitHub and Slack effects;
- explicit unknown outcomes;
- agent-facing MCP surface without commit authority.

Not yet certified or implemented fully:

- real rootless Docker and Podman host certification;
- controlled allow-listed egress for agent containers;
- signed and isolated third-party adapter processes;
- production secret-manager/workload-identity integrations;
- production PostgreSQL transaction adapter;
- multi-user authentication and authorization;
- distributed coordinator/server mode;
- complete fuzzing and external security audit.

## 15. Canonical implementation stack

```text
Language                 Go 1.23+
Daemon transport         private HTTP over Unix socket
Agent bridge             MCP stdio JSON-RPC
Durable state            SQLite WAL
Git staging              detached worktrees + exact tree identities
Container boundary       Docker/Podman rootless source implementation
Provider adapters        built-in Go adapters
Verification             deterministic contract engine
Evidence                 SQLite metadata + content-addressed blobs
UI                       deferred
```

## 16. Repository layout

```text
cmd/
├── futurediff/          CLI for trusted local operations
├── futurediffd/         authoritative daemon
└── futurediff-mcp/      unprivileged MCP stdio bridge

internal/
├── adapters/
│   ├── githubbranch/
│   ├── githubdraft/
│   └── slackoutbox/
├── api/
├── app/
├── credentials/
├── domain/
├── futurepack/
├── ledger/
├── mcpbridge/
├── runtimeoci/
├── staging/
└── verification/
```

## 17. Architecture decisions added in Tasks 011–013

```text
ADR-034 Deterministic commit identity before publication
ADR-035 Durable effect dependency DAG
ADR-036 Create-only GitHub branch publication
ADR-037 Slack messages use a durable outbox
ADR-038 MCP bridge excludes approval and commit authority
```

## 18. Next architectural priorities

The narrow public MVP is now primarily blocked by validation and release work rather than a missing core transaction path:

1. certify rootless Docker and Podman on real Linux hosts;
2. run real GitHub test-repository and Slack test-workspace integration tests;
3. publish a reproducible benchmark against direct, permission-only, and sandbox-only baselines;
4. add install scripts, SBOM, signed checksums, release provenance, and upgrade tests;
5. provide tested MCP client configuration examples;
6. harden the SQLite dependency and migration path;
7. add adversarial and fuzz testing.

The architecture statement remains:

> Existing agents decide what to do. FutureDiff controls whether and how those decisions become persistent reality.

---

# Architecture update v1.8 — Tasks 014–018

## Host certification plane

The OCI runtime is now accompanied by a separate host-certification command. Enforced-mode support in source and enforced-mode certification on a host are different facts. Certification binds the runtime identity, rootless status, image digest, workspace isolation checks, secret isolation checks, and report digest.

## Release plane

All commands consume one build-identity package. Tagged Linux releases are generated only after race tests and include eight binaries, SPDX 2.3 SBOM, file checksums, architecture, README, and provenance notes.

## Benchmark plane

The first benchmark is a deterministic effect-semantic model. It is explicitly isolated from future real-agent benchmarks, preventing synthetic results from being represented as model-quality or token-performance evidence.

## Ledger maintenance plane

The ledger verifies embedded migration identities, supports full integrity checks, and creates online SQLite backups rather than copying a live WAL database. Backups are reopened and validated before publication and recorded by digest.

## Demonstration plane

The default demonstration uses the same Go transaction, staging, verification, approval, and commit services as the daemon. It proves the central property without provider dependencies: the approved FutureDiff ref changes and the live checkout does not.

## 19. Post-MVP hardening additions (Tasks 019–023)

### Controlled adapter egress

Built-in GitHub API and Slack adapters use a daemon-owned HTTPS transport. It permits only declared hostname, port, method, and path combinations, disables redirects and environment proxies, resolves DNS before dialing, and rejects private or special-purpose addresses. This blocks common credential-redirection and DNS-rebinding paths.

### Agent-specific integration profiles

`futurediff-integrate` generates official local-stdio MCP entries for OpenCode and Hermes Agent. Both profiles expose the same non-release MCP surface. The OpenCode strict profile blocks its direct edit and shell capabilities; the Hermes profile uses an explicit include-list and disables resources, prompts, and parallel tool calls.

### Fault-injection boundary

The SQLite bridge contains an internal-only fault-injection hook. Release checks prove rollback on pre-commit failure, backup atomicity under injected failure, and corrupted-backup detection. Fault injection is never enabled for the production repository.

### Release provenance

Release packages include an in-toto Statement v1 using SLSA provenance v1. Tagged GitHub workflows additionally request a signed GitHub artifact attestation through `actions/attest`. Embedded provenance and signed attestation are distinct evidence layers.

---

# Architecture update v2.4 — Task 024

## Unified certification plane

FutureDiff now has a target-oriented certification orchestrator. It binds local
integration, rootless OCI, GitHub readiness, Slack readiness, OpenCode, Hermes,
and signed release-attestation checks into one machine-readable report.

Certification has four explicit states:

```text
pass     check was executed and its invariant held
fail     check was executed and the invariant did not hold
blocked  required external prerequisite was unavailable
skip     target or optional check was intentionally not executed
```

A blocked target cannot make the report certified. Source implementation,
readiness, and live mutation certification remain separate claims.

Provider readiness uses the daemon-owned controlled egress transport and accepts
credential environment-variable names rather than values. The general suite does
not create branches, pull requests, or Slack messages; externally visible
certification requires an explicit disposable-resource workflow.

---

# Architecture update v2.9 — Tasks 025–029

## Installation plane

Installation is expressed as a declarative JSON plan before files are written. The plan identifies every binary and user-service file, allowing dry-run review and future package-manager integration. The installer creates a private data root but never enables credentials or provider mutations automatically.

Linux uses a systemd user service with `NoNewPrivileges`, `PrivateTmp`, a read-only home boundary, and an explicit writable FutureDiff root. macOS uses a launchd user agent. Starting or enabling those services remains an explicit operator action.

## Platform plane

Platform support is no longer inferred from whether the Go compiler can emit a binary. Linux amd64 is the supported primary target. Linux arm64 and macOS amd64/arm64 are experimental native test targets. Windows is explicitly unsupported until named-pipe transport, Windows service management, SQLite packaging, and enforced credential isolation are designed and tested.

## Agent-measurement plane

Real-agent performance evidence uses a versioned measured-run record. Token counts, model calls, repair turns, wall time, verification time, compute time, and effect outcomes are supplied by the benchmark runner or provider records. FutureDiff computes overhead relative to a named baseline but never fabricates missing metrics.

## Release-consumer verification plane

Release verification is offline-first. A consumer can safely extract an archive, verify `SHA256SUMS`, validate the SPDX document, and verify every in-toto/SLSA subject digest. A GitHub-signed attestation can be required as an additional trust layer.

## Explicit provider-mutation certification plane

Provider readiness and provider mutation certification are distinct. The mutation certifier requires a hard confirmation phrase and dedicated disposable resources. GitHub certification creates an unreachable commit, temporary `futurediff-cert/*` branch, and draft PR, then closes and deletes the reachable resources. Slack certification posts and deletes a marked message. Missing credentials produce `blocked`; cleanup failure produces `fail`.

---

# Architecture update v3.5 — Tasks 030–035

## Tamper-evident ledger plane

Events are now linked by a per-transaction SHA-256 chain. Opening the ledger backfills legacy rows and refuses an existing-chain mismatch. The chain is an integrity detector, not an externally anchored signature.

## Invariant-audit plane

A read-only auditor combines SQLite integrity with semantic cross-table checks: approval bindings, repository materialization, receipts, unknown states, terminal-state cleanup, dependency cycles, and event-chain continuity.

## Retention plane

Terminal runtime artifacts can be removed through a deterministic, confirmation-gated plan. Durable metadata, retention evidence, and published Git refs remain intact. Paths outside the managed transaction root fail closed.

## Operator-diagnostics plane

The doctor command joins environment, permission, runtime, daemon, and ledger readiness into one machine-readable report without resolving secrets.

## Contract-compatibility plane

The local daemon exposes a versioned API inventory and digest. Agent-safe operations are explicitly separated from approval, commit, recovery, and abort authority. Clients can refuse a daemon whose contract digest differs.

---

# Architecture update v4.0 — Tasks 036–040

## Portable forensic plane

A transaction can now be exported as a verified `.futurepack`. The export contains durable non-secret projections and the exact patch artifact when available. Content addressing, archive path validation, duplicate-reference rejection, and defense-in-depth token redaction make the artifact suitable for review, incident response, and reproducible bug reports.

## Offline recovery plane

Ledger restoration is a separate offline operation. The supplied backup is copied before validation, bound to an expected SHA-256, checked through SQLite integrity, migration identity, semantic audit, and event-chain verification, and published only after a consistent pre-restore backup of the current ledger exists.

## Projection replay plane

The event chain is now used for more than row-tamper detection. A read-only replay engine reconstructs transaction, approval, and effect status and compares those results with materialized SQLite projections. This is a consistency check, not a claim that all FutureDiff storage is fully event sourced.

## Configuration assurance plane

Credentials, verification contracts, measured benchmark records, installer plans, and strict OpenCode profiles can be linted before use. Authority-expanding OpenCode profiles fail closed.

## Semantic compatibility plane

API contracts now support structural validation and semantic diffing. Endpoint removal, operation-identity changes, major-version changes, and any agent-safety reclassification are incompatible. Additive endpoints remain visible without being incorrectly classified as breaking.


---

# Architecture update v4.5 — Tasks 041–045

## Adapter assurance plane

EffectSpec now has a reusable lifecycle conformance suite. Adapter authors can prove descriptor validity, preparation identity, preview and verification evidence, commit receipts, status semantics, abort behavior, and compensation behavior without loading their code into the trusted daemon.

## Policy-understanding plane

Verification contracts can be explained and simulated before execution. The simulator resolves the dependency DAG and models blocked checks, but it has no authority to write verification evidence or transition a transaction.

## Recovery-assurance plane

The recovery planner codifies the no-blind-retry rule as executable scenarios. Ambiguous provider outcomes require status queries; re-arm requires proof that no mutation occurred; insufficient evidence escalates.

## Privacy-preserving observability plane

Operational metrics contain aggregate counts and bounded status labels only. Transaction identifiers, repository paths, provider destinations, message content, and credential identities are excluded.

## Supportability plane

A verified support bundle combines build metadata, operator diagnostics, semantic ledger audit, aggregate metrics, and the API contract. The ledger, transaction artifacts, and credential configuration contents remain outside the bundle.


---

# Architecture update v5.5 — Tasks 051–055

## Maintenance coordination plane

A digest-protected local maintenance state can freeze every daemon mutation while preserving read-only health and inspection. Automatic expiry limits accidental lockout. The boundary is local to the daemon and is not described as a distributed lock.

## Evidence confidentiality plane

Runtime stdout, stderr, and structured execution evidence may be written directly as AES-256-GCM artifacts. Transaction, execution, and artifact identity are authenticated as associated data. SQLite, Git patches, filenames, and historical artifacts remain outside this encryption boundary.

## Operator key-lifecycle plane

Ed25519 approval keys now support overlap rotation, controlled cutover, revocation, and a final-key lockout guard. The daemon loads its trust set at startup, so keyring changes require restart/reload before taking effect.

## Human timeline plane

Durable event sequences can be transformed into JSON, Markdown, or Mermaid timelines without including event payload bodies, patches, provider messages, or secrets. A timeline digest binds the rendered chronology to its durable inputs.

## Threat-regression plane

A local threat-model command executes named controls for agent authority, provider egress, maintenance integrity, evidence authentication, signed approvals, terminal-state immutability, and approval-key revocation. It is a regression harness, not an external security certification.

## Tasks 056–060 architecture additions

### Configuration attestation
FutureDiff can bind deployment configuration identity without copying file contents. A snapshot records absolute path, required/optional state, permissions, byte size, and SHA-256 digest.

### Approval quorum
A daemon may require a quorum policy in addition to a trusted Ed25519 keyring. The policy counts distinct approver identities, can restrict the allowed set, and can require named approvers. Approval bundles are bound to one transaction ID and one approval digest.

### Evidence keyring
Runtime evidence encryption now supports one active AES-256-GCM write key and enabled historical decrypt keys. Rotation changes new writes immediately without rewriting historical artifacts.

### Incident reconstruction
The incident reporter composes the durable diff, timeline, replay result, effect state, receipts, and ledger audit into one read-only digest-bound report.

### Bounded daemon lifecycle
SIGTERM begins a drain. New mutations receive `503 daemon_draining`; active mutations are allowed to complete up to the configured timeout. The server then performs graceful shutdown and removes its socket and PID file.

---

# Architecture update v6.5 — Tasks 061–065

## Operator receipt plane

High-value local operator actions can be recorded as append-only Ed25519-signed receipts. Each receipt is bound to the previous receipt digest, producing a per-installation chain. The receipt store is separate from the transaction ledger and does not contain private keys or provider secrets.

## Policy-driven retention plane

Retention now accepts a versioned policy that constrains terminal age, candidate count, and bytes before the existing confirmation-gated deletion mechanism can run. Policy evaluation and deletion remain separate actions.

## Dependency-graph plane

The composed future can be projected as a portable graph containing repository, verification, approval, and external-effect dependencies. This graph is suitable for future UI consumption but remains independently useful as JSON, Mermaid, or DOT.

## SLO assurance plane

Local service-level objectives are evaluated from aggregate metrics, semantic audit results, daemon reachability, and maintenance status. SLO evaluation has no mutation authority.

## Readiness-gate plane

A manifest-driven release gate combines ledger audit, SLO results, local API identity, maintenance state, and optional operator receipt verification. External-system certifications remain distinct and cannot be implied by a local pass.

---

# Architecture update v7.0 — Tasks 066–070

## Local-principal authentication plane

On Linux, the HTTP-over-Unix-socket server derives PID, UID, and GID from `SO_PEERCRED` and authorizes the UID before routing any request. Socket mode `0600` remains defense in depth. FutureDiff does not accept identity from an HTTP header.

## Durable request-safety plane

Mutation requests may carry an idempotency key. The durable record is namespaced by authenticated principal and binds method, path, and exact request digest. A completed response can be replayed after daemon restart; mismatched reuse and concurrent in-progress reuse fail closed. Strict 1 MiB body/response limits and single-value JSON decoding reduce parser and memory abuse.

## Secret-preflight plane

Every verification run can begin with a deterministic patch scan that examines only added lines. High-confidence credential patterns become a required failed verification result and prevent command checks from executing. Evidence contains rule identifiers, line numbers, fingerprints, and redacted previews—not raw credentials.

## Resource-governance plane

A versioned quota policy limits open transactions, prepared effects, runtime executions, patch bytes, changed paths, and verification-check cardinality. Limits are enforced before the next durable or expensive resource is allocated.

## Local mutation-audit plane

Each local mutation attempt is recorded with kernel-authenticated principal, method, route, status, and SHA-256 identities for request material and idempotency key. Request bodies, response bodies, and raw keys are intentionally excluded.

---

# Architecture update v7.5 — Tasks 071–075

## Single-writer daemon plane

Each data root has a kernel-held exclusive daemon lock. PID files remain useful for signalling but no longer carry the mutual-exclusion guarantee. Lock metadata is inspectable without treating stale bytes as ownership.

## Tamper-evident API evidence plane

Payload-free API access events now form one global SHA-256 chain ordered by SQLite sequence. The verifier detects gaps, previous-digest mismatches, and canonical event-digest changes. The local head is not presented as an external transparency anchor.

## Principal abuse-control plane

Authenticated principals receive independent read and mutation token buckets plus a maximum active-mutation count. Rejections occur before expensive handlers and are themselves represented in the API audit ledger.

## Signed deployment-configuration plane

Credential metadata, approval policy, evidence keys, verification preflight policy, quotas, and rate limits may require detached Ed25519 sidecars. File bytes are verified before configuration parsing. The signing keyring is a separately protected bootstrap trust root.

## Filesystem trust plane

The daemon treats the private data root as part of its trusted computing base. Ownership, permissions, symlink safety, and top-level file types are checked before locking or opening the ledger.

---

# Architecture update v8.0 — Tasks 076–080

## Controlled storage-maintenance plane

SQLite maintenance is now an explicit offline operation protected by the daemon's kernel lock. Apply mode creates a consistent backup, requires a healthy semantic audit, checkpoints the WAL, runs optimization/analysis/compaction, and requires another healthy audit before success is reported.

## Signed integrity-checkpoint plane

A consistent ledger backup can be bound to aggregate transaction event-chain heads, the mutation API audit head, and an optional operator-receipt head. The resulting material digest is signed with an operator Ed25519 key. This provides portable local evidence but is not described as trusted timestamping or external transparency.

## Lease-hygiene plane

Expired coordinator leases are inspectable and removable only while the daemon is offline. Deletion is confirmation-gated and constrained by the persisted expiry timestamp, preserving all live fencing tokens.

## Request-correlation plane

Every local HTTP request carries a bounded correlation identity. The daemon returns it and mutation audit records bind it into the existing global API hash chain. Correlation identity is deliberately separate from peer authentication, authorization and durable idempotency.

## Repository-admission plane

Authenticated local principals are no longer sufficient to stage any reachable Git path when a repository policy is configured. Admission binds canonical repository roots, Git object formats, detached-head behavior and dirty-worktree policy before a transaction workspace is allocated.

---

# Architecture update v8.5 — Tasks 081–085

## Abandoned-transaction lifecycle plane

A versioned expiry policy may identify only explicitly safe pre-commit states (`active`, `sealed`, `failed_verification`, `ready`, and `stale`). Evaluation is deterministic and dry-run-first. Apply requires the daemon's exclusive lock, an exact confirmation phrase, state/revision revalidation, normal abort cleanup, and a durable expiry action record. In-flight commit, compensation, reconciliation, and manual-intervention states cannot be selected.

## Idempotency-retention plane

Durable API idempotency responses are retained independently from payload-free mutation audit evidence. A separate policy distinguishes completed-response retention from abandoned `in_progress` reservations. Deletion is offline, confirmation-gated, bounded by candidate count, and records only aggregate cleanup evidence. Plans expose SHA-256 identities rather than raw principals or idempotency keys.

## Storage-pressure plane

An optional daemon storage policy evaluates filesystem free bytes/percentage plus ledger and managed-runtime growth. Reads remain available under pressure, but mutations fail before durable reservation or handler execution with HTTP `507`. The policy is observable through health and can be protected by the existing signed-configuration bootstrap.

## Machine-readable API-description plane

The authoritative local API contract now generates a deterministic OpenAPI 3.1 document. Every operation carries the canonical operation ID and a FutureDiff-specific agent-safety declaration. The document is available over the private daemon socket and through an offline command; validation rejects missing, unexpected, renamed, or authority-reclassified operations.

## Verified backup-catalog plane

Ledger backup records can be reconciled against actual files without trusting paths or metadata blindly. Catalog evaluation enforces a canonical backup root, non-symlink regular files, exact size/SHA-256 identity, and SQLite integrity on a disposable verification copy. Retention is bounded, dry-run-first, offline, and confirmation-gated; modified or missing backups block apply.
