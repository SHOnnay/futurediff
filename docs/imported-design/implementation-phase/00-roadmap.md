# Implementation-Phase Roadmap

## Purpose

This phase converts the architecture package into implementation-ready contracts. The target is not more ideas; the target is a buildable core with frozen interfaces and measurable guarantees.

## Priority rule

FutureDiff should enter implementation through the engine, not the UI.

Current priority order:

1. core specs;
2. state machine;
3. approval and resource identity contracts;
4. conformance + benchmark contract;
5. repository bootstrap;
6. adapters and vertical slice;
7. usable UI later.

## Deferred UI note

For later UI work, keep the visual system plain and usable:
- black;
- matte black;
- gray;
- low-decoration, utility-first layout.

No cinematic or fancy visual work is part of the current phase.

## Step sequence

### Step 01 — Freeze EffectSpec 0.1
**Goal**: define the adapter contract every effectful integration must obey.

**Deliverables**:
- lifecycle contract;
- effect classes;
- adapter support levels;
- required metadata fields;
- approval-relevant hashes and evidence hooks.

**Exit criteria**:
- one spec document exists;
- lifecycle terms are unambiguous;
- support levels are explicit enough for conformance tests.

### Step 02 — Freeze transaction state machine
**Goal**: remove ambiguity from transaction/effect transitions and recovery semantics.

**Deliverables**:
- transaction states;
- effect states;
- allowed transitions;
- retry rules;
- `UNKNOWN` and manual-intervention rules.

### Step 03 — Freeze approval snapshot contract
**Goal**: guarantee that approval applies to an exact prepared future.

**Deliverables**:
- approval payload fields;
- hash inputs;
- resource version pins;
- policy version binding;
- invalidation rules.

### Step 04 — Freeze canonical resource URI contract
**Goal**: make locking, drift detection, and audit trails consistent across adapters.

**Deliverables**:
- URI format rules;
- resource namespace rules;
- normalization rules;
- examples for git, postgres, github, slack.

### Step 05 — Freeze conformance and benchmark contract
**Goal**: force adapters and releases to prove the claims.

**Deliverables**:
- adapter lifecycle tests;
- idempotency tests;
- recovery tests;
- unsupported-effect tests;
- benchmark success criteria.

### Step 06 — Bootstrap implementation repository
**Goal**: start coding against frozen contracts, not loose prose.

**Deliverables**:
- repo skeleton;
- spec folder;
- control-plane skeleton;
- test harness skeleton;
- first vertical slice targets.

### Step 07 — MVP technical research plan
**Goal**: remove implementation-critical unknowns before the engine expands.

**Deliverables**:
- stack decision record;
- interception strategy decision;
- MVP adapter reality matrix;
- recovery/idempotency research note;
- implementation risk register.

## Current step

**Active now: Step 06 + Step 07 — Bootstrap repo live, durable coordinator state started**

The core specs are frozen. Research fixed the MVP interception path, control-plane stack, adapter support reality, recovery discipline, local staging model, and benchmark feasibility split. The bootstrap repo now has machine-readable schemas, Go control-plane interfaces, a working wrapper-boundary spike, Postgres lease/recovery proof, deterministic local promotion recovery, disposable Postgres migration preview, GitHub duplicate-recovery, Slack ambiguous-send handling, a staged verification gate that aborts before promotion while preserving inspectable evidence, a first cross-tool preparation flow spanning repo staging, Postgres preview, GitHub preparation, and Slack preparation, benchmark smoke for file-change failure, destructive shell containment, migration failure, duplicate retry, and stale GitHub drift, a verified local contributor bootstrap stack, exportable artifact-store-backed bundles for both benchmark and cross-tool evidence, a multi-effect commit seam with partial-commit reconciliation and compensation, exact approval invalidation on material drift, coordinator-owned approval state, a Docker-backed staged-command path wired through the gateway, a coordinator-owned transition engine for approval/commit/reconcile/compensate/manual-intervention paths, and a first durable Postgres-backed coordinator state store. The next useful move is broader benchmark coverage and deeper coordinator transition ownership with more production-shaped policy wiring.