# Conformance and Benchmark Contract 0.1 Draft

## Status

Draft for implementation-phase Step 05.

## Objective

This document freezes the proof contract for FutureDiff adapters, coordinator behavior, and release claims. FutureDiff is not allowed to claim transactional safety, recovery, or reduced damage unless the implementation proves those behaviors under repeatable tests and benchmark scenarios.

This is the release gate between design and implementation credibility.

## Design principles

1. Every strong guarantee MUST have a test.
2. Every adapter MUST pass the same baseline conformance suite before it is trusted.
3. Benchmark scenarios MUST include failure, ambiguity, and recovery paths, not only happy paths.
4. Release claims MUST be backed by measured evidence, not demos.
5. Unsupported behavior MUST fail closed and be tested explicitly.

## Scope

This contract defines:
- shared adapter conformance requirements;
- coordinator/state-machine conformance requirements;
- benchmark scenario set;
- benchmark metrics;
- minimum publishable release gate.

It does not define:
- implementation language;
- CI vendor;
- dashboard presentation of benchmark results.

## Test layers

FutureDiff MUST ship with five test layers:

1. schema and contract validation;
2. adapter conformance tests;
3. coordinator and recovery tests;
4. vertical-slice integration tests;
5. comparative benchmark scenarios.

A release is not credible if any layer is missing.

## 1. Schema and contract validation

Every implementation build MUST validate:

- EffectSpec documents;
- transaction transition tables;
- approval snapshot schema;
- resource URI normalization rules;
- exported `.futurepack` manifest shape when present.

These tests prove the machine-readable contracts do not drift from the prose contracts.

## 2. Adapter conformance suite

Every trusted adapter MUST pass one shared test kit.

### Required adapter categories in MVP

- git/filesystem
- container/runtime
- Postgres
- GitHub
- Slack

### Required adapter conformance groups

#### 2.1 Describe contract tests
Must prove:
- required metadata fields exist;
- support level is declared;
- resource URI patterns are declared;
- unsupported lifecycle operations are explicit, not implicit.

#### 2.2 Prepare tests
Must prove:
- valid inputs produce stable `prepared_handle` and `prepared_fingerprint` semantics;
- invalid inputs fail with normalized error classes;
- prepare does not release final side effects.

#### 2.3 Preview tests
Must prove:
- preview corresponds to the prepared artifact;
- preview output is stable within one prepared version;
- limitations are declared when preview is weaker than exact prepare/commit.

#### 2.4 Verify tests
Must prove:
- adapter-specific preconditions are checked;
- verification failures are surfaced deterministically;
- verify does not create hidden side effects.

#### 2.5 Commit tests
Must prove:
- commit uses the approved prepared fingerprint or declared weaker mode;
- idempotency keys are reused when supported;
- commit emits a durable receipt or `TIMEOUT_AMBIGUOUS`;
- commit never silently downgrades support level.

#### 2.6 Abort tests
Must prove:
- abort is retry-safe before commit;
- abort removes staged state when claimed;
- abort failure is explicit and recoverable.

#### 2.7 Status tests
Must prove:
- status maps provider-native state into normalized FutureDiff state;
- ambiguous states map to `UNKNOWN`;
- status is sufficient for reconciliation decisions.

#### 2.8 Compensation tests
Must prove:
- compensation exists only when declared;
- compensation references the original commit receipt;
- compensation outcome is distinct from rollback or abort.

#### 2.9 Unsupported-path tests
Must prove:
- unsupported tools or operations fail closed;
- the adapter does not attempt best-effort side effects when marked unsupported.

## 3. Coordinator and state-machine conformance suite

The control plane MUST pass tests for the Step 02 and Step 03 contracts.

### 3.1 Transaction transition tests
Must prove:
- legal transitions succeed;
- illegal transitions are rejected;
- terminal states are immutable;
- lease loss routes through `RECONCILING`.

### 3.2 Approval invalidation tests
Must prove that approval is invalidated when any material field changes:
- prepared fingerprint;
- preview fingerprint;
- resource version;
- policy bundle hash;
- verification bundle hash;
- effect set;
- commit order;
- support level.

### 3.3 Unknown-state handling tests
Must prove:
- ambiguous outcomes enter `UNKNOWN`;
- `UNKNOWN` always routes to `RECONCILING`;
- commit is not blindly retried after ambiguity;
- evidence is preserved for manual intervention.

### 3.4 Locking tests
Must prove:
- canonical resource URIs drive lock acquisition;
- conflicting transactions are blocked or arbitrated by policy;
- lock loss during commit prevents unauthorized continuation.

### 3.5 Recovery tests
Must prove:
- restart during `COMMITTING` recovers deterministically;
- restart during `ABORTING` recovers deterministically;
- restart during `COMPENSATING` recovers deterministically;
- ledger state is sufficient to resume safely.

## 4. Vertical-slice integration suite

FutureDiff MUST ship at least one end-to-end MVP flow that exercises multiple effect domains in one transaction.

### Canonical MVP flow

```text
Modify repository code
+ run disposable database migration
+ prepare GitHub pull request
+ prepare Slack notification
```

This flow MUST be tested in both:
- success path;
- failure path.

### Required vertical-slice assertions

- no second agent run is required for commit;
- staged diff is inspectable before approval;
- verification gates commit;
- commit ordering is honored;
- crash recovery resumes or compensates safely;
- final receipts and exported evidence are internally consistent.

## 5. Benchmark scenario contract

The benchmark suite MUST compare these baselines:

1. direct agent execution;
2. agent with permission prompts;
3. agent in sandbox only;
4. agent with FutureDiff.

FutureDiff is not required to win every metric. It is required to show a better safety/correctness tradeoff with explicit measured cost.

## Required benchmark scenarios

### B1. Destructive shell command
Goal: prove policy block or containment.

Expected result:
- direct execution may damage state;
- FutureDiff blocks or safely contains the effect.

### B2. File changes followed by test failure
Goal: prove failed verification prevents real side effects.

Expected result:
- staged repository diff exists;
- transaction aborts;
- no GitHub or Slack side effect occurs.

### B3. Migration failure
Goal: prove database staging and verification catch bad migrations.

Expected result:
- disposable DB fails;
- real DB not changed;
- transaction never commits external notifications.

### B4. Stale GitHub base branch
Goal: prove drift invalidates approval or blocks commit.

Expected result:
- freshness check fails;
- re-approval or re-prepare is required.

### B5. Duplicate API retry
Goal: prove idempotency handling.

Expected result:
- one durable effect only;
- retry returns prior receipt or resolved status.

### B6. Crash after first external commit
Goal: prove reconciliation and ordered continuation or compensation.

Expected result:
- transaction enters `RECONCILING`;
- remaining effects continue safely or compensation begins;
- no duplicate external effects occur.

### B7. Provider timeout ambiguity
Goal: prove `UNKNOWN` handling.

Expected result:
- ambiguous effect enters `UNKNOWN`;
- no blind commit retry;
- reconciliation resolves or escalates.

### B8. Unexpected outbound domain or tool
Goal: prove fail-closed behavior.

Expected result:
- unsupported effect is rejected;
- transaction does not proceed silently.

### B9. Compensation failure
Goal: prove honest degraded handling.

Expected result:
- partial compensation is recorded;
- transaction escalates to `FAILED_MANUAL_INTERVENTION` when budget is exhausted.

### B10. Manual drift during pending approval
Goal: prove approval snapshot invalidation.

Expected result:
- transaction returns to `ACTIVE`;
- old approval snapshot remains as audit artifact;
- commit is blocked until re-approval.

## 6. Metrics contract

Every benchmark run MUST record at least:

- task completion rate;
- irreversible-effect failures;
- duplicate effects;
- successful abort rate;
- successful recovery rate;
- successful compensation rate;
- false blocks;
- approvals required;
- wall-clock overhead;
- compute overhead;
- token overhead;
- diff accuracy;
- unsupported-effect detection rate.

### Metric definitions

#### `duplicate effects`
Count of externally visible repeated side effects produced by retry, crash, or coordinator restart.

#### `successful abort rate`
Fraction of transactions that correctly end in `ABORTED` before any committed external effect.

#### `successful recovery rate`
Fraction of interrupted transactions that reach the policy-correct final state without duplicate or hidden effects.

#### `false blocks`
Number of transactions blocked by policy, lock, or adapter classification even though the effect set was actually supported and safe under current rules.

#### `diff accuracy`
How accurately the staged and committed artifacts match the approved prepared version.

## 7. Release gate

A public MVP release MUST NOT ship unless the repository can demonstrate all of the following:

1. failed verification prevents real external side effects;
2. commit does not require a second agent run;
3. duplicate retries do not create duplicate effects in supported idempotent paths;
4. interrupted transactions recover or compensate according to policy;
5. unsupported effects fail closed;
6. approval invalidates on material drift;
7. at least three effect domains compose in one transaction;
8. measured token and latency overhead are reported.

## 8. Evidence format

Each conformance or benchmark run SHOULD emit a durable evidence bundle containing:

- run metadata;
- contract version refs;
- transaction/event log refs;
- receipts;
- verification outputs;
- approval snapshot refs when used;
- normalized metrics;
- pass/fail verdicts.

This bundle SHOULD be exportable into `.futurepack` or an equivalent benchmark artifact.

## 9. Minimum CI expectations

Before marking an adapter or coordinator change ready, CI SHOULD run:

- contract/schema tests;
- adapter conformance tests for changed adapters;
- coordinator state-machine tests;
- one cross-tool vertical slice;
- benchmark smoke subset for regressions.

Nightly or scheduled runs SHOULD execute the full benchmark matrix.

## 10. Required failure honesty

When a test reveals weaker guarantees than claimed, FutureDiff MUST do one of:
- downgrade the adapter support level;
- narrow the product claim;
- block the feature from trusted mode.

The system MUST NOT keep a strong marketing claim on top of a weak test result.

## 11. Suggested repository layout impact

Step 05 implies these repo areas need first-class ownership:

```text
specs/
adapters/shared-testkit/
verifier/contracts/
benchmarks/
examples/
```

Conformance is a product feature here, not auxiliary QA.

## Exit criteria for Step 05

Step 05 is complete when this draft is turned into:
- a final conformance spec;
- shared adapter test kit definitions;
- coordinator contract test plan;
- benchmark scenario definitions with metrics schema;
- release-gate checklist used by implementation.

## Immediate next step after this document

Bootstrap the implementation repository around the frozen contracts so coding begins against these specs instead of against informal design prose.
