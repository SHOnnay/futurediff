# Implementation Bootstrap Plan

## Status

Starting implementation-phase Step 06.

## Objective

This document turns the frozen design contracts into an implementation bootstrap plan. The goal is to create the smallest repository shape and engineering sequence that lets FutureDiff start building the engine safely.

This is not the full product plan. It is the bridge from spec work into real code.

## Bootstrap principles

1. Code MUST start from frozen contracts, not from fresh interpretation.
2. The repository MUST be organized around the engine, not the UI.
3. Machine-readable specs MUST land before broad adapter work.
4. Shared test harnesses MUST exist before adapter count expands.
5. The first vertical slice MUST prove safety behavior, not styling.

## What Step 06 must produce

A Step 06-complete bootstrap should leave the project with:

- a clean implementation repository layout;
- machine-readable contract skeletons;
- control-plane package boundaries;
- shared adapter testkit layout;
- verifier and benchmark scaffolding;
- one clearly defined MVP vertical slice target.

## Bootstrap order

### 1. Create repo skeleton
Start with these top-level areas:

```text
futurediff/
├── specs/
├── control-plane/
├── staging/
├── adapters/
├── verifier/
├── integrations/
├── benchmarks/
├── examples/
└── ui/
```

`ui/` exists only as a placeholder for now. No meaningful UI work belongs in the bootstrap milestone.

## 2. Create machine-readable spec area first

Before coordinator or adapter code expands, create:

```text
specs/
├── effectspec/
│   ├── effectspec.schema.json
│   ├── lifecycle.md
│   └── support-levels.md
├── transactions/
│   ├── transaction-states.json
│   ├── effect-states.json
│   └── transition-rules.json
├── approval/
│   └── approval-snapshot.schema.json
├── resources/
│   └── canonical-resource-uri.md
└── benchmarks/
    └── metrics.schema.json
```

### Why this comes first
The coordinator, adapters, CLI, and tests will all depend on these artifacts. If the specs are not machine-readable early, code will drift immediately.

## 3. Create control-plane skeleton

Start with interfaces and domain models, not full implementation.

Recommended package split:

```text
control-plane/
├── gateway/
├── coordinator/
├── ledger/
├── locks/
├── approvals/
├── reconciliation/
├── policy/
├── credentials/
└── domain/
```

### Minimum first interfaces
Define these before real orchestration logic:

- `TransactionStore`
- `EffectStore`
- `LedgerWriter`
- `LockManager`
- `ApprovalStore`
- `PolicyEvaluator`
- `Reconciler`
- `AdapterRegistry`
- `AdapterRunner`

### Minimum first domain types
Define:
- transaction state enum;
- effect state enum;
- transition record;
- effect binding record;
- approval snapshot ref;
- resource lock record;
- receipt ref;
- retry budget config.

## 4. Create adapter workspace with shared test kit first

Recommended structure:

```text
adapters/
├── shared-testkit/
├── filesystem/
├── git/
├── runtime/
├── postgres/
├── github/
└── slack/
```

### Rule
No adapter should be considered “real” until it runs against `shared-testkit/`.

## 5. Create verifier and benchmark scaffolding

```text
verifier/
├── contracts/
├── runners/
├── evidence/
└── freshness/

benchmarks/
├── baseline-direct/
├── baseline-prompts/
├── baseline-sandbox/
├── cross-tool/
├── crash-recovery/
└── adversarial/
```

### Initial goal
Do not fill these out completely yet. Create the scaffolding and the first benchmark definitions so implementation has a target to code against.

## 6. Define the first vertical slice before broad coding

The first implementation slice should be fixed now:

```text
repo code change
+ disposable Postgres migration
+ GitHub PR preparation
+ Slack message preparation
+ verification failure path
+ zero real external side effects
```

This slice is the implementation center of gravity.

## Recommended implementation sequence inside Step 06

### Milestone 6.1 — contract conversion
Build:
- JSON Schemas;
- transition tables;
- canonical resource normalization helper contract;
- metrics schema.

### Milestone 6.2 — domain skeleton
Build:
- domain enums and records;
- interface boundaries;
- coordinator orchestration skeleton.

### Milestone 6.3 — shared testkit skeleton
Build:
- adapter conformance harness entry points;
- fixture conventions;
- mock receipt/state helpers.

### Milestone 6.4 — persistence skeleton
Build:
- Postgres schema draft for transactions/effects/ledger/locks;
- object-store abstraction for artifacts and evidence.

### Milestone 6.5 — first runnable integration seam
Build:
- one gateway entry path;
- one local staging path;
- one benchmark-smoke transaction path.

## Recommended stack decisions to freeze early

These should be settled before code fans out:

- **Control plane language**: Go
- **Metadata store**: Postgres
- **Artifact store**: S3-compatible blob store
- **Initial interception path**: one primary wrapper/proxy path only
- **Runtime staging**: git worktrees + container runtime + disposable Postgres

If any of those change later, bootstrap cost rises sharply.

## Implementation boundaries for bootstrap

### In scope
- repo skeleton;
- machine-readable contract stubs;
- domain and interface skeletons;
- testkit skeleton;
- benchmark scaffolding;
- first vertical-slice target definition.

### Out of scope
- full dashboard;
- cinematic UI;
- multi-tenant SaaS concerns;
- broad provider catalog;
- production-scale deployment automation.

## Definition of done for Step 06

Step 06 is in good shape when:

- repository structure exists and is coherent;
- contract files have machine-readable starting points;
- control-plane packages and interfaces are fixed enough for parallel work;
- shared adapter testkit location and conventions are fixed;
- benchmark scaffold exists;
- first vertical slice is named and bounded.

## Risks to avoid

- starting with adapters before shared testkit exists;
- building coordinator behavior from prose only;
- letting UI or branding work consume bootstrap time;
- supporting multiple interception paths before one path works;
- over-designing hosted/SaaS concerns before the engine proves itself.

## Immediate next deliverable after this plan

Use the research plan to freeze the MVP stack and adapter reality, then create the initial machine-readable specs and repository skeleton from this document.
