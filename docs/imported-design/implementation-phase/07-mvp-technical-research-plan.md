# MVP Technical Research Plan

## Status

Starting implementation-phase Step 07.

## Objective

This document defines the research work required before FutureDiff implementation expands. The goal is to remove the major unknowns that could poison the first implementation cycle: wrong stack choices, fake adapter guarantees, weak interception strategy, or hand-wavy recovery assumptions.

This is research with a hard output: decisions and implementation constraints.

## Research principles

1. Research MUST answer build-critical questions, not collect random notes.
2. Every research item MUST end in a decision, a rejected option, or an explicit unresolved risk.
3. MVP research MUST stay scoped to the first vertical slice.
4. Claims about adapters MUST be based on real provider/runtime capabilities, not wishful abstraction.
5. Research should narrow options fast; it should not create endless branching.

## Required research outputs

Step 07 should produce:

- one MVP stack decision record;
- one interception strategy decision;
- one adapter reality matrix for MVP integrations;
- one recovery/idempotency design note;
- one risk register for implementation blockers.

## Research tracks

## Track A — Control-plane stack decisions

### Questions to answer
- Is Go the right control-plane language for the coordinator and gateway?
- Is Postgres sufficient as the first source of truth?
- Is a separate queue needed in MVP, or can workers remain DB-driven initially?
- What object storage abstraction should be used first?

### Required outcome
Freeze:
- control-plane language;
- persistence baseline;
- worker model;
- artifact storage baseline.

### Good-enough evidence
- implementation complexity comparison;
- concurrency/recovery fit;
- local-dev ergonomics;
- deployment simplicity for MVP.

## Track B — Interception strategy

### Candidate paths
- wrapper CLI around an agent;
- MCP proxy;
- framework-specific hook/plugin path.

### Questions to answer
- Which path gives the fastest credible MVP?
- Which path best guarantees FutureDiff remains the effect boundary?
- Which path is easiest to debug during recovery failures?
- Which path minimizes framework lock-in while still being buildable now?

### Required outcome
Choose one primary interception path for MVP and explicitly defer the others.

### Recommendation to test first
A wrapper/proxy path is usually the sanest MVP start. Supporting multiple interception styles immediately is a bad use of time.

## Track C — Adapter reality matrix

This is the most important research track.

For each MVP adapter, answer these questions with evidence:

- Can it actually `prepare`?
- Can it actually `preview`?
- Can it commit the exact prepared version?
- What is the freshness check?
- What is the idempotency mechanism?
- What is the compensation path?
- What creates `UNKNOWN` state?
- What resource URIs must it emit?

### MVP adapters to research
- Git/filesystem
- container/runtime
- Postgres
- GitHub
- Slack

### Required output format
Use one row per adapter with at least these columns:

| Adapter | prepare | preview | exact commit | freshness check | idempotency | compensation | unknown triggers | support level | main risk |
|---|---|---|---|---|---|---|---|---|---|

### Why this matters
This is where fake product claims usually begin. If GitHub or Slack only support weaker guarantees than hoped, that needs to be admitted now and reflected in support level and benchmark scope.

## Track D — Recovery and idempotency patterns

### Questions to answer
- What retry and idempotency patterns best fit FutureDiff’s commit model?
- Which operations can safely reuse idempotency keys?
- What must happen after timeout before retry?
- What evidence is required to leave `UNKNOWN`?
- Which recovery loops need strict budgets?

### Good research targets
Study:
- Stripe-style idempotency handling;
- saga/compensation patterns;
- durable workflow recovery ideas from systems like Temporal or Cadence.

The goal is not to copy their stack. The goal is to steal the right invariants.

### Required outcome
Produce a short design note that hardens:
- idempotency key scope;
- retry policy shape;
- reconciliation loop behavior;
- manual-intervention threshold.

## Track E — Local staging and runtime reality

### Questions to answer
- What is the exact local runtime for MVP staging?
- How will worktrees be created and cleaned up?
- What container runtime assumptions are safe?
- How will disposable Postgres instances be provisioned locally and in CI?
- What is the minimum artifact storage need during MVP?

### Required outcome
One practical local-dev plan that engineering can actually run without ceremony.

## Track F — Benchmark feasibility

### Questions to answer
- Which failure scenarios are easy to automate first?
- Which scenarios need provider mocks versus real providers?
- Which metrics can be captured reliably in MVP?
- What evidence format should each run export?

### Required outcome
A split between:
- fully automatable benchmark scenarios;
- partially mocked scenarios;
- later scenarios that should not block MVP bootstrap.

## Research order

Do the tracks in this order:

1. Interception strategy
2. Control-plane stack decisions
3. Adapter reality matrix
4. Recovery/idempotency note
5. Local staging/runtime plan
6. Benchmark feasibility split

This order reduces wasted design churn.

## Time-box guidance

Research should be bounded.

Suggested rule:
- spend enough time to make a confident build decision;
- stop before the note turns into speculative architecture theater.

If a question cannot be fully answered in research, record:
- current best decision;
- what must be proven by a spike;
- what risk remains.

## Decision record template

For each research decision, capture:

- `decision_id`
- `topic`
- `decision`
- `alternatives_considered`
- `why_this_wins_for_mvp`
- `known_tradeoffs`
- `follow_up_spike_needed`
- `owner`
- `date`

## Risk register template

Track at least:

- `risk_id`
- `description`
- `impact`
- `likelihood`
- `mitigation`
- `blocked_area`
- `needs_spike`

## What counts as success for Step 07

Step 07 is successful when:

- the MVP stack is chosen;
- one interception path is chosen;
- adapter guarantees are honest and bounded;
- recovery/idempotency rules are sharper than generic prose;
- implementation can start without major unknowns in the first vertical slice.

## What not to waste time on

Do not spend this research phase on:
- visual style systems;
- hosted multi-tenant product decisions;
- non-MVP provider ecosystems;
- advanced marketing demos;
- broad performance tuning before the engine exists.

## Immediate next deliverable after this plan

Produce concrete research notes and decision records for the MVP stack, interception path, and adapter reality matrix, then use those decisions to instantiate the Step 06 repository skeleton.
