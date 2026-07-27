# Benchmark Feasibility Split

## Status

Initial research-phase split.

## Purpose

This document splits benchmark scenarios into what can be automated early, what likely needs mocks or controlled harnesses, and what should not block MVP bootstrap.

The goal is to stop benchmark ambition from derailing implementation while still preserving the proof bar.

## Category A — fully automatable early

These should be built first and should block MVP bootstrap drift.

### A1. File changes followed by test failure
Why early:
- uses local staging only;
- proves zero external side effects on failed verification.

### A2. Migration failure in disposable Postgres
Why early:
- strong local control;
- validates DB preview discipline.

### A3. Duplicate retry on local/staged effect
Why early:
- easiest place to prove idempotent coordinator behavior.

### A4. Crash during local promotion / recovery
Why early:
- strong control over crash injection;
- validates ledger and reconciliation model.

### A5. Approval invalidation on local drift
Why early:
- mostly coordinator/state-machine logic;
- no provider unpredictability required.

## Category B — automatable with real provider accounts

These are valuable in MVP, but should run in controlled test accounts/workspaces.

### B1. GitHub PR creation with stale base branch
Need:
- test repo;
- test branches;
- controlled branch drift.

### B2. GitHub ambiguous create/update recovery
Need:
- controlled timeout or response-loss simulation;
- search-based reconciliation checks.

### B3. Slack outbox release and receipt capture
Need:
- test workspace/channel;
- ability to inspect posted message receipts.

### B4. Slack ambiguous send handling
Need:
- test workspace;
- simulated timeout/lost response harness;
- search/metadata reconciliation checks.

These are still MVP-relevant, but they are more fragile than local-only scenarios.

## Category C — partially mocked or harness-driven first

These should be built with strong mocks/harnesses before trying to rely on real-provider chaos.

### C1. Provider timeout ambiguity
Why mocked first:
- deterministic ambiguity injection is easier than waiting for real provider weirdness.

### C2. Compensation failure
Why mocked first:
- easier to force cleanly and repeatedly;
- good for manual-intervention path tests.

### C3. Unexpected outbound tool/domain
Why mocked first:
- primarily tests policy and coordinator fail-closed behavior.

### C4. Lock-loss during commit
Why mocked first:
- deterministic lease loss simulation is better than accidental concurrency bugs.

## Category D — later, not bootstrap-blocking

These matter, but they should not delay Step 06 bootstrap.

### D1. Cross-framework comparison beyond one primary path
### D2. Large-scale performance benchmarking
### D3. Rich multi-tenant hosted deployment benchmarks
### D4. Broad provider matrix outside Git/Postgres/GitHub/Slack
### D5. Adversarial prompt-injection benchmark breadth beyond one or two curated cases

## MVP benchmark minimum set

The minimum serious benchmark set should be:

1. file changes + test failure;
2. disposable DB migration failure;
3. approval invalidation on drift;
4. local duplicate-retry handling;
5. local crash recovery;
6. GitHub stale-base drift;
7. one GitHub ambiguous recovery case;
8. one Slack outbox release case.

That is enough to prove the project is not just a UI shell.

## Real-provider guidance

When using real providers in MVP benchmarks:
- use test repos/workspaces only;
- isolate credentials per benchmark environment;
- prefer cleanup through compensation or disposable targets;
- never benchmark against uncontrolled production resources.

## Evidence expectations by category

### Category A
Should emit full deterministic evidence bundles by default.

### Category B
Should emit receipts, provider IDs, and redacted audit bundles.

### Category C
Should emit harness traces that show injected ambiguity/failure conditions clearly.

### Category D
Can remain planned until the engine and first benchmark set are stable.

## Practical recommendation

Start by building Category A and the easiest parts of Category B.

That gives FutureDiff:
- real proof of the core engine;
- at least some real external-provider evidence;
- no dependency on ambitious chaos harnesses before the basics work.

## Immediate next use of this split

Use it to:
- prioritize the first benchmark scaffold directories;
- decide what requires real provider test accounts;
- keep bootstrap milestones honest and bounded.
