# Bootstrap Spike List

## Purpose

These are the smallest spikes that de-risk the first implementation cycle.

## Priority 1

### S-001 Wrapper boundary spike — completed
Proved:
- one staged command runs through FutureDiff;
- effect IDs and transaction IDs are persisted;
- staged diff is inspectable before commit;
- commit applies the stored patch without rerunning the command.

### S-002 Postgres lease/recovery spike — completed
Proved:
- one worker can claim a Postgres-backed lease;
- renew it;
- lose it after expiry;
- another worker can reacquire it and resume from persisted state.

### S-003 Local patch-promotion spike — completed
Proved:
- exact staged patch promotion works;
- crash during promotion can be reconciled deterministically through stored patch evidence and recovery state.

## Priority 2

### S-004 Disposable Postgres migration spike — completed
Proved:
- migration preview runs in a disposable DB;
- rollback test and schema diff are captured;
- prod-facing commit path remains freshness-check driven.

### S-005 GitHub duplicate-recovery spike — completed
Proved:
- ambiguous PR create can be reconciled through search/status without blind duplicate creation.

### S-006 Slack ambiguous-send spike — completed
Proved:
- outbox + metadata/search is good enough for MVP best-effort dedupe.


### S-009 Verification gate spike — completed
Proved:
- staged verification can fail after patch capture but before promotion;
- the transaction aborts cleanly;
- the staged patch remains inspectable as evidence;
- commit rejects the aborted transaction without promoting repo changes.

## Priority 3

### S-007 Artifact store spike — completed
Proved:
- large evidence stays out of the control-plane database path and lands in durable content-addressed files;
- receipts/previews/export artifacts survive store reopen and bundle export.

### S-008 Local-dev stack spike — completed
Proved:
- one developer can run the current MVP bootstrap stack with acceptable setup cost;
- required local binaries, artifact storage, worktree execution, and disposable Postgres preview all pass one probe.

### S-010 Cross-tool vertical slice spike — completed
Proved:
- one preparation flow spans repo staging, Postgres preview, GitHub PR preparation, and Slack notification preparation;
- passing verification produces an inspectable transaction awaiting approval;
- failing verification preserves evidence and leaves zero outward GitHub/Slack side effects.

### S-011 File-change failure benchmark smoke — completed
Proved:
- direct execution leaves the repo changed after failure;
- the FutureDiff path aborts after verification and leaves the repo unchanged;
- the comparison records baseline-vs-guarded timing for the same class of scenario.

### S-012 Destructive shell containment smoke — completed
Proved:
- direct execution damages the source repo;
- the FutureDiff path contains the destructive shell effect inside staging;
- the staged patch preserves the destructive diff for inspection.

### S-013 Benchmark evidence export spike — completed
Proved:
- benchmark reports and normalized metrics can be stored as durable artifacts;
- a `.futurepack` bundle can be exported and re-opened from the stored artifacts.

### S-014 Migration failure smoke — completed
Proved:
- direct execution can leave a real database partially changed before failure;
- the FutureDiff preview path blocks before touching the real database or calling GitHub/Slack.

### S-015 Cross-tool evidence export spike — completed
Proved:
- one prepared cross-tool flow can be exported as a `.futurepack` bundle;
- the bundle captures repo, Postgres, transaction, and prepared external-effect evidence together.

### S-016 Duplicate retry smoke — completed
Proved:
- a naive direct retry can create duplicate GitHub pull requests after ambiguity;
- the FutureDiff path resolves to one durable effect through recovery.

### S-017 Stale GitHub drift smoke — completed
Proved:
- a pinned base SHA can be checked before PR creation;
- drift blocks the FutureDiff path before outward PR creation.

### S-018 Multi-effect commit orchestration seam — completed
Proved:
- repo promotion, GitHub creation, and Slack send can run as one commit flow;
- freshness is checked before outward effects;
- ambiguous external commits recover through adapter recovery paths.

### S-019 Partial-commit reconciliation seam — completed
Proved:
- commit progress can be persisted across repo and external-effect boundaries;
- a crash after the first external commit can be reconciled without duplicating the already-durable effect.

### S-020 Approval invalidation seam — completed
Proved:
- approval snapshots can bind to exact prepared state;
- material drift invalidates approval before repo promotion or outward effects.

### S-021 Containerized runtime hardening spike — completed
Proved:
- a hardened Docker-compatible run plan can be built and tested with explicit isolation defaults;
- runtime unavailability is surfaced honestly instead of silently ignored.

### S-022 Compensation policy path — completed
Proved:
- a Slack failure after repo and GitHub commit can trigger a concrete GitHub compensation path;
- compensation state is persisted instead of being hidden behind a generic failure.

### S-023 Coordinator-owned approval state — completed
Proved:
- approval snapshot refs and invalidation state can live behind a control-plane-owned store boundary.

### S-024 Container runtime wiring — completed
Proved:
- Docker-backed execution is no longer only a runtime seam; it is wired into the staged command boundary through injectable executors.

### S-025 Coordinator transition wiring — completed
Proved:
- approval and invalidation can now drive transaction transitions through a coordinator-owned engine;
- effect states and ledger transitions can move with the transaction instead of staying only in helper-layer prose.

### S-026 Coordinator Postgres state path — completed
Proved:
- transaction state, effect state, and ledger transitions can persist in Postgres;
- the coordinator engine can run approval/commit/reconcile transitions against the durable Postgres-backed stores.
## Rule

Do not start broad adapter implementation before Priority 1 spikes are understood.
