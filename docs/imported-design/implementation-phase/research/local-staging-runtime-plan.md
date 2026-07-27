# Local Staging and Runtime Plan

## Status

Initial research-phase plan.

## Purpose

This plan defines the practical local runtime for MVP development and testing. It is intentionally boring. The goal is to make FutureDiff runnable on a developer machine and in CI without inventing a heavyweight platform too early.

## Bootstrap reality note

The current bootstrap repo now proves an honest local path with two runtime layers:

- default host-shell staged execution;
- git worktrees;
- disposable Postgres;
- filesystem-backed artifact storage;
- optional Docker-backed staged execution through injected command executors.

The Docker path is now wired, but it is still optional rather than the default runtime mode.

## MVP local stack

Use this default local stack:

- Go toolchain
- local Postgres
- local blob store emulator or filesystem-backed blob adapter
- git worktrees
- container runtime
- disposable Postgres instance for staged DB checks

## Runtime choices

## 1. Git worktrees for repository staging

Use git worktrees as the default local code-isolation mechanism.

Why:
- exact diff visibility;
- easy cleanup;
- branch-aware staging;
- strong fit with “no second LLM run” commit behavior.

### Rules
- one transaction gets one worktree namespace;
- worktree path must be transaction-derived and deterministic enough for debugging;
- stale worktrees must be prunable and auditable.

## 2. Container runtime for command/test isolation

Use one mainstream local container runtime path first.

Recommendation:
- target Docker-compatible behavior first.

Reason:
- contributor familiarity;
- CI portability;
- lower MVP variance.

Avoid starting with runtime abstraction theater across Docker, Podman, and other systems before one path works well.

## 3. Disposable Postgres for DB preview

Use a disposable Postgres instance for:
- migration dry runs;
- rollback checks;
- fixture validation;
- schema diff capture.

The disposable DB may run:
- in a container; or
- in a dedicated local test service.

For MVP, containerized disposable Postgres is the sanest path.

## 4. Artifact storage in local mode

For local development, allow two modes:

### Mode A — filesystem-backed artifact store
Good for the fastest local bootstrap.

### Mode B — S3-compatible emulator
Good for integration and export testing.

Recommendation:
- start local dev with filesystem-backed storage;
- use S3-compatible emulation in integration/CI paths where export behavior matters.

## Directory and namespace model

## Local transaction workspace layout

A practical local structure should look like:

```text
.futurediff/
├── transactions/
│   └── <transaction-id>/
│       ├── worktree/
│       ├── artifacts/
│       ├── previews/
│       ├── evidence/
│       └── receipts/
├── runtime/
├── logs/
└── cache/
```

This directory does not have to be public product branding. It just needs to be deterministic, inspectable, and easy to clean.

## Cleanup rules

The local system MUST support:
- transaction workspace cleanup after terminal states;
- retention for debug mode;
- pruning stale worktrees and staged artifacts after crashes.

Cleanup should never destroy active transaction state.

## Local developer workflow target

The simplest useful path should be:

```text
futurediff run -- <agent-command>
futurediff inspect <tx>
futurediff verify <tx>
futurediff commit <tx>
futurediff recover <tx>
```

That is enough for MVP local ergonomics.

## CI runtime target

CI should run the same basic model with minimal drift:

- ephemeral checkout/worktree;
- containerized runtime;
- disposable Postgres;
- filesystem or S3-emulated artifacts;
- Postgres metadata DB for coordinator tests.

Avoid having CI exercise a completely different runtime philosophy than local dev.

## Non-goals for MVP runtime

Do not add these yet:
- Kubernetes-native staging;
- multi-node worker orchestration;
- cloud-managed artifact dependencies as a hard local prerequisite;
- support for every container runtime under the sun.

## Early implementation spikes to run

1. **Worktree lifecycle spike**
   - create, use, inspect, and prune transaction worktrees safely.

2. **Container staging spike**
   - run commands inside staged runtime and export artifacts without rerun.

3. **Disposable Postgres spike**
   - apply migration, rollback test, and capture schema diff locally.

4. **Artifact path spike**
   - prove large evidence stays out of Postgres and survives recovery.

## Known risks

- local runtime may feel heavy if too many services are mandatory at once;
- worktree cleanup bugs can leave stale disk state;
- container/runtime drift between local and CI can poison benchmark trust;
- filesystem-backed local artifacts may hide bugs that appear in object-store mode.

## Decisions frozen by this plan

- git worktrees are the default local repo-staging mechanism;
- Docker-compatible runtime is the first supported command isolation path;
- disposable Postgres is mandatory for DB preview/testing;
- local artifact storage may start filesystem-backed, but the product model still targets S3-compatible storage.

## Immediate next use of this plan

Use it to define the Step 06 repository bootstrap and the first local integration seam for:
- worktree creation;
- staged runtime execution;
- disposable DB verification;
- artifact persistence.
