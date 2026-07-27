# Task 007 — Full Go Migration and Runnable Daemon

**Status:** Completed  
**Architecture version:** 0.7  
**Primary language:** Go 1.23

## Objective

Move the entire trusted FutureDiff core from the earlier Rust-oriented workspace and Node reference daemon into one executable Go codebase without discarding the previously designed transaction, staging, verification, approval, and recovery contracts.

## Why the language changed

Go was selected for faster implementation and contributor velocity, strong standard-library coverage for local daemons and process control, straightforward concurrency, and built-in race testing.

The decision does not assume that Go is universally faster than Rust at runtime. The immediate advantage is that the project can now be compiled, executed, and tested in the available environment.

## Delivered

### Go daemon and CLI

```text
cmd/futurediffd
cmd/futurediff
```

The daemon listens on a Unix-domain socket with mode `0600`. No TCP listener is opened.

### Transaction domain

Implemented:

- transaction states and legal transitions;
- reconciliation transitions back to `READY` or `STALE`;
- effect-state vocabulary;
- transaction, workspace, patch, verification, and materialized-ref models;
- cryptographic IDs and SHA-256 digests.

### Durable SQLite ledger

Implemented migrations for:

- transactions;
- effects and dependencies;
- events;
- evidence;
- approvals;
- leases and fencing fields;
- receipts and compensations;
- verification runs and check results;
- workspaces;
- staged patches;
- materialized repository refs.

Implemented operations:

- create transaction and workspace atomically;
- compare-and-set transitions;
- separate material revision;
- patch sealing;
- verification-result persistence;
- independent outcome recalculation;
- digest-bound approval;
- commit start and finalization;
- reconciliation state entry;
- event retrieval.

### SQLite implementation caution

The build environment could not download external Go modules. Task 007 therefore includes a minimal cgo binding to the system SQLite C API.

This code is isolated in `internal/ledger/sqlite.go` and is intentionally not exposed as a generic SQL package.

Follow-up:

- review a maintained SQLite driver;
- pin and verify the dependency;
- replace the bootstrap bridge without changing migrations or repository tests.

### Git staging runtime

Implemented:

- canonical repository and Git common-directory inspection;
- pinned base commit and source ref;
- dirty-source rejection by default;
- explicit stage-from-HEAD policy;
- rejection of tracked symlinks, submodules, and Git content filters;
- detached transaction worktrees;
- binary-capable full-index patches;
- patch SHA-256;
- exact Git tree OID;
- material approval digest;
- patch-tampering detection;
- stale-source blocking;
- exact tree reproduction in an integration worktree;
- atomic FutureDiff-ref publication;
- live-checkout preservation;
- evidence-preserving workspace abort.

### Deterministic verification

Implemented:

- contract version `0.1`;
- dependency DAG;
- lexical ready-check ordering;
- cycle and missing-dependency rejection;
- required-check outcome calculation;
- file-exists, file-absent, and file-SHA-256 checks;
- symlink and traversal rejection;
- optional cooperative local command executor;
- daemon default that disables local command execution;
- verification, check, evidence, and cache-key digests.

### OCI runtime package

Ported the secure runtime plan to Go:

- Docker/Podman probing;
- rootless status discovery;
- digest-pinned image validation;
- `--pull=never`;
- read-only root;
- network disabled by default;
- all capabilities dropped;
- no-new-privileges;
- CPU, memory, PID, and temporary-filesystem limits.

A real OCI runtime was unavailable, so this package is source- and unit-tested but not host-certified.

### EffectSpec Go package

Added a public Go lifecycle contract:

```text
describe
prepare
preview
verify
commit
abort
compensate
status
```

The descriptor validates mutation, commit, compensation, reversibility, and preview declarations.

### Recovery

Implemented and tested:

1. transaction enters `COMMITTING`;
2. exact FutureDiff Git ref is published;
3. ledger finalization is simulated as missing;
4. recovery enters `NEEDS_RECONCILIATION`;
5. existing ref tree is compared with the approved staged tree;
6. matching tree finalizes `COMMITTED`.

When no ref exists:

- pinned source returns the transaction to `READY`;
- moved source returns it to `STALE` and invalidates approval.

## API delivered

```text
GET  /v1/health
POST /v1/transactions
GET  /v1/transactions/{id}
POST /v1/transactions/{id}/seal
POST /v1/transactions/{id}/verify
GET  /v1/transactions/{id}/approval-material
POST /v1/transactions/{id}/approve
POST /v1/transactions/{id}/commit
POST /v1/transactions/{id}/recover
POST /v1/transactions/{id}/abort
GET  /v1/transactions/{id}/events
```

## Executed validation

```text
gofmt: PASS
go vet ./...: PASS
go test -race ./...: PASS
go test -cover ./...: PASS
go build ./cmd/...: PASS
Unix-socket daemon lifecycle: PASS
SQLite migrations and CAS behavior: PASS
Git exact-tree materialization: PASS
Published-ref recovery: PASS
Live checkout unchanged: PASS
Socket mode 0600: PASS
```

Process smoke result:

```text
health=ok
status=committed
live=current
future=future
socket_mode=600
```

## Coverage snapshot

```text
effectspec              55.6%
internal/api             59.9%
internal/app             44.2%
internal/domain          47.4%
internal/ledger          39.9%
internal/runtimeoci       8.3%
internal/staging         63.2%
internal/verification    59.7%
```

The low OCI coverage is expected because Docker and Podman are unavailable. It is the priority for Task 008.

## Explicit limitations

Task 007 does not yet prove enforced execution.

Not delivered:

- agent isolation inside rootless OCI;
- live OCI command evidence;
- credential brokerage;
- default-deny host egress enforcement;
- GitHub/Slack/database adapters;
- external effect idempotency and compensation;
- Windows named pipes;
- UI.

The daemon rejects `mode=enforced` rather than giving a false security guarantee.

## Repository migration result

The primary repository now contains Go source only for the trusted core. The earlier Rust crates and Node reference daemon are not part of the Task 007 primary package.

Small shell and CI files remain operational tooling, not trusted application logic.

## Next task

### Task 008 — Rootless OCI composition in Go

Required work:

- connect `internal/runtimeoci` to the application service;
- create a sanitized execution copy separate from the Git staging worktree;
- run command checks in a rootless container;
- remove credentials and host home access;
- default network to none;
- capture bounded stdout/stderr and structured termination evidence;
- always discard the execution copy;
- never synchronize after timeout, cancellation, workspace-limit breach, or runtime failure;
- certify Docker and Podman behavior on real rootless hosts;
- enable enforced mode only after certification.
