# DR-002 — MVP Control-Plane Stack

## Status

Accepted for MVP research phase.

## Decision ID

`DR-002`

## Topic

Control-plane stack for FutureDiff MVP.

## Decision

FutureDiff MVP should use:

- **Go** for the control plane;
- **Postgres** as the source of truth for transaction metadata, ledger state, locks, and recovery coordination;
- **DB-driven workers** before adding a separate queue;
- **S3-compatible blob storage** for artifacts, evidence, patches, exports, and large previews.

## Alternatives considered

### 1. Go + Postgres + DB-driven workers + S3-compatible storage
Chosen.

### 2. TypeScript/Node control plane
Deferred.

### 3. Python control plane
Deferred.

### 4. Queue-first architecture in MVP
Rejected for MVP.

## Why this wins for MVP

### Go fits the coordinator problem well
FutureDiff’s core is a coordination and recovery engine. The hard part is not template rendering or frontend velocity. The hard part is:

- durable state transitions;
- concurrent workers;
- leases and recovery loops;
- explicit timeout handling;
- long-lived service behavior;
- small deployable runtime footprint.

Go is a good fit for that shape.

### Postgres is enough for MVP truth storage
Postgres can credibly handle:

- transactions;
- effects;
- transition ledger;
- advisory or row-backed locks;
- approval snapshot metadata;
- recovery scanning;
- retry budgets;
- worker claim/lease tables.

Using Postgres first keeps recovery reasoning simpler because the source of truth stays concentrated.

### DB-driven workers are simpler than queue-first
A queue-first design adds moving parts early:

- queue delivery semantics;
- separate visibility timeouts;
- duplicate claim behavior;
- second operational truth source.

MVP should prefer a DB-driven worker model where workers:

- poll or claim runnable work from Postgres;
- renew leases in Postgres;
- append state transitions before and after meaningful actions.

This is enough until throughput or latency proves otherwise.

### S3-compatible storage keeps evidence handling clean
FutureDiff will generate artifacts that do not belong inline in Postgres:

- large diffs;
- DB dumps;
- provider previews;
- receipts;
- benchmark evidence bundles;
- `.futurepack` exports.

S3-compatible storage is a boring, correct fit.

## Why TypeScript is deferred
TypeScript is attractive for SDKs and tooling, but it is not the best first control-plane choice because:

- long-lived worker/recovery behavior tends to become dependency-heavy fast;
- runtime and deployment surface grows quickly;
- recovery-heavy concurrency logic is less boring than it should be.

TypeScript should still exist for SDKs and integration helpers.

## Why Python is deferred
Python is strong for experimentation, but it is a weaker first fit for the coordinator because:

- the engine is not research-code-heavy;
- concurrency and service hardening become less boring than needed;
- single-binary distribution is worse.

Python remains useful later for SDKs, examples, or benchmark helpers.

## Why queue-first is rejected for MVP
Queue-first sounds scalable but is the wrong first tradeoff here.

FutureDiff first needs:

- correctness;
- determinism;
- debuggable recovery;
- one obvious source of state truth.

A queue can be added later if DB-driven workers become the bottleneck. MVP should not pay that complexity upfront.

## MVP implementation shape implied by this decision

```text
Go control plane
+ Postgres source of truth
+ DB-driven worker loops
+ S3-compatible artifact storage
+ local staged runtime
```

## Local development implications

The default local-dev stack should target:

- Go toolchain;
- local Postgres;
- local object-store emulator or filesystem-backed blob adapter;
- git worktrees;
- container runtime;
- disposable Postgres for staging tests.

## Known tradeoffs

- Go schema/model generation workflow is less flexible than some TS-first stacks;
- DB-driven workers are not the final scale model;
- local object-store emulation adds one more moving part in dev;
- TypeScript-heavy contributors may prefer a JS control plane.

These are acceptable because MVP needs engine correctness more than stack popularity.

## Required follow-up spikes

1. **Lease and recovery spike**
   - prove one worker can claim, renew, lose, and recover transaction work using Postgres only.

2. **Artifact store spike**
   - prove evidence and preview artifacts can move out of Postgres cleanly.

3. **Local-dev stack spike**
   - prove the stack can run with minimal ceremony on one developer machine.

## Main risks introduced

- poor schema boundaries could still make the Go codebase rigid if domain types are not designed carefully;
- DB polling can become sloppy if worker claim semantics are underdesigned;
- object-store abstraction can be overengineered too early.

These are design discipline problems, not stack blockers.

## Owner

FutureDiff architecture track.

## Date

2026-07-26

## Resulting next research item

Build the MVP adapter reality matrix against this stack and interception model so support levels are honest before repository bootstrap begins.
