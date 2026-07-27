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
