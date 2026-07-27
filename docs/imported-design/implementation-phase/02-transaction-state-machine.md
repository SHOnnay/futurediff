# Transaction State Machine 0.1 Draft

## Status

Draft for implementation-phase Step 02.

## Objective

This document freezes the transaction and effect state model for FutureDiff. It defines the allowed states, legal transitions, retry rules, recovery semantics, `UNKNOWN` handling, and manual-intervention boundaries.

This is the authoritative control-plane behavior for:
- transaction coordination;
- effect lifecycle orchestration;
- crash recovery;
- duplicate retry handling;
- approval invalidation;
- compensation entry.

## Design principles

1. State transitions MUST be explicit and persisted.
2. Every transition MUST be append-only in the ledger before side effects continue.
3. Ambiguity MUST be represented as `UNKNOWN`, never guessed away.
4. Recovery is a normal state path, not an exception bolt-on.
5. Transaction state is derived from effect state plus coordinator intent, not from UI assumptions.
6. A transaction may finish in safe failure; that is a valid outcome.

## Scope

This spec covers:
- transaction state;
- effect state;
- transition guards;
- retry semantics;
- reconciliation semantics;
- manual-intervention terminal behavior.

It does not yet define:
- approval snapshot schema;
- resource URI schema;
- exact persistence table layout.

## Transaction states

### `NEW`
Transaction record exists but no effectful work has been accepted.

### `ACTIVE`
The gateway is accepting tool calls and building staged effects.

### `VERIFYING`
Transaction-level verification is running against the prepared future.

### `AWAITING_APPROVAL`
Verification passed for the current prepared snapshot and approval is required.

### `READY_TO_COMMIT`
The exact approved snapshot is locked and may proceed to commit.

### `COMMITTING`
The coordinator is executing the ordered effect commit plan.

### `COMMITTED`
All committed-intent effects reached durable success and the transaction completed successfully.

### `ABORTING`
The coordinator is discarding prepared but uncommitted state.

### `ABORTED`
All abortable prepared state has been discarded and no commit occurred.

### `RECONCILING`
The system is resolving an incomplete or ambiguous transaction after crash, timeout, lease loss, or restart.

### `COMPENSATING`
At least one effect committed, later progress failed, and policy selected compensation rather than forward completion.

### `COMPENSATED`
Compensation completed to the extent defined by policy and no further automatic action remains.

### `FAILED_MANUAL_INTERVENTION`
Automatic recovery is no longer safe or possible. A human must inspect and decide.

## Effect states

### `DECLARED`
The tool call was accepted for lifecycle handling, but prepare has not succeeded yet.

### `PREPARED`
The effect has staged state or a prepared handle and can participate in preview/verification.

### `PREVIEWED`
A preview exists for the currently prepared fingerprint.

### `VERIFIED`
Effect-local and transaction-relevant checks passed for the current prepared version.

### `APPROVED`
The effect is part of an approved transaction snapshot.

### `COMMITTING`
A commit attempt is in progress or was in progress when control was lost.

### `COMMITTED`
The effect has a durable commit receipt.

### `ABORTED`
Prepared state was discarded and no commit receipt exists.

### `COMPENSATING`
A compensation attempt is in progress.

### `COMPENSATED`
A compensation receipt exists.

### `UNKNOWN`
The system cannot yet prove whether commit succeeded, failed safely, or partially completed.

## State ownership rules

- Only the transaction coordinator may change transaction state.
- Adapters propose effect outcomes; the coordinator normalizes them into effect state.
- UI, CLI, and integrations are read/write clients of the coordinator API, not alternate state owners.

## Transaction transition rules

### Allowed transitions

```text
NEW -> ACTIVE
ACTIVE -> VERIFYING
ACTIVE -> ABORTING
VERIFYING -> ACTIVE
VERIFYING -> AWAITING_APPROVAL
VERIFYING -> ABORTING
AWAITING_APPROVAL -> READY_TO_COMMIT
AWAITING_APPROVAL -> ACTIVE
AWAITING_APPROVAL -> ABORTING
READY_TO_COMMIT -> COMMITTING
READY_TO_COMMIT -> ACTIVE
READY_TO_COMMIT -> ABORTING
COMMITTING -> COMMITTED
COMMITTING -> RECONCILING
COMMITTING -> COMPENSATING
ABORTING -> ABORTED
ABORTING -> RECONCILING
RECONCILING -> COMMITTING
RECONCILING -> ABORTING
RECONCILING -> COMPENSATING
RECONCILING -> COMMITTED
RECONCILING -> ABORTED
RECONCILING -> COMPENSATED
RECONCILING -> FAILED_MANUAL_INTERVENTION
COMPENSATING -> COMPENSATED
COMPENSATING -> RECONCILING
COMPENSATING -> FAILED_MANUAL_INTERVENTION
```

Any transition not listed above MUST be rejected and logged as an internal error.

### Terminal transaction states

- `COMMITTED`
- `ABORTED`
- `COMPENSATED`
- `FAILED_MANUAL_INTERVENTION`

Terminal states MUST be immutable except for metadata enrichment that does not change semantic state.

## Effect transition rules

### Allowed transitions

```text
DECLARED -> PREPARED
DECLARED -> ABORTED
PREPARED -> PREVIEWED
PREPARED -> VERIFIED
PREPARED -> ABORTED
PREPARED -> UNKNOWN
PREVIEWED -> VERIFIED
PREVIEWED -> ABORTED
PREVIEWED -> UNKNOWN
VERIFIED -> PREPARED
VERIFIED -> APPROVED
VERIFIED -> ABORTED
VERIFIED -> UNKNOWN
APPROVED -> PREPARED
APPROVED -> COMMITTING
APPROVED -> ABORTED
APPROVED -> UNKNOWN
COMMITTING -> COMMITTED
COMMITTING -> UNKNOWN
UNKNOWN -> COMMITTED
UNKNOWN -> ABORTED
UNKNOWN -> COMPENSATING
UNKNOWN -> FAILED_MANUAL_INTERVENTION
COMMITTED -> COMPENSATING
COMPENSATING -> COMPENSATED
COMPENSATING -> FAILED_MANUAL_INTERVENTION
```

### Terminal effect states

- `COMMITTED`
- `ABORTED`
- `COMPENSATED`
- `FAILED_MANUAL_INTERVENTION` at the transaction level only

Effect rows themselves MUST NOT invent a separate effect terminal beyond the normalized set above. If a provider has richer details, store them as metadata.

## Transition guards

### `NEW -> ACTIVE`
Allowed only after:
- transaction ID exists;
- ledger stream initialized;
- coordinator lease acquired.

### `ACTIVE -> VERIFYING`
Allowed only when:
- all accepted effects are at least `PREPARED` or already `ABORTED`;
- no unsupported effect remains unresolved;
- required resource locks are still held.

### `VERIFYING -> AWAITING_APPROVAL`
Allowed only when:
- verification contract passed;
- no effect is `UNKNOWN`;
- approval is required by policy or effect class.

### `VERIFYING -> ACTIVE`
Used when:
- verification failed but repair is still allowed;
- prepared state changed during verification;
- the agent must modify the staged future and re-verify.

### `AWAITING_APPROVAL -> READY_TO_COMMIT`
Allowed only when:
- approval snapshot exists;
- approval record is valid;
- policy version still matches;
- no prepared fingerprint or resource version drift was detected.

### `READY_TO_COMMIT -> COMMITTING`
Allowed only when:
- commit order is frozen;
- commit lease is valid;
- all freshness checks required by support level passed;
- no effect remains unsupported.

### `COMMITTING -> COMMITTED`
Allowed only when every effect in commit scope is either:
- `COMMITTED`; or
- already terminal and intentionally excluded by policy.

### `COMMITTING -> RECONCILING`
Required when:
- process crashes or loses lease during commit;
- an adapter returns `TIMEOUT_AMBIGUOUS`;
- any effect enters `UNKNOWN`.

### `COMMITTING -> COMPENSATING`
Allowed only when:
- at least one effect is `COMMITTED`;
- a later effect failed or became unsafe to continue;
- policy selected compensation over forward completion.

### `ABORTING -> ABORTED`
Allowed only when every non-committed effect is safely discarded or was never prepared.

### `ABORTING -> RECONCILING`
Used when abort itself becomes ambiguous or the process crashes while aborting.

### `RECONCILING -> FAILED_MANUAL_INTERVENTION`
Required when:
- ambiguity cannot be resolved within retry budget;
- compensation repeatedly fails;
- provider state remains unknowable;
- human review is mandatory by policy.

## Derived transaction state rules

The coordinator MUST derive transaction state using these rules:

1. If any effect is `UNKNOWN`, transaction state MUST be `RECONCILING` unless already terminal.
2. If any effect is `COMPENSATING`, transaction state MUST be `COMPENSATING`.
3. If at least one effect is `COMMITTED` and another commit-path effect cannot proceed safely, transaction MUST enter `COMPENSATING` or `RECONCILING`, never `ABORTING`.
4. `ABORTED` is valid only when no effect has a commit receipt.
5. `COMMITTED` is valid only when no effect remains `UNKNOWN`, `COMMITTING`, or `COMPENSATING`.

## Approval invalidation rules

A transaction in `AWAITING_APPROVAL` or `READY_TO_COMMIT` MUST move back to `ACTIVE` when any of these change materially:

- prepared fingerprint;
- preview fingerprint;
- resource version;
- policy version;
- commit order;
- effect set;
- support level of any effect.

This invalidation MUST be explicit in the ledger.

## Retry rules

### General rule
Blind retry is prohibited for `commit` after ambiguous outcomes.

### Safe retries
The coordinator MAY safely retry:
- `prepare`
- `preview`
- `verify`
- `abort`
- `status`
- `compensate` when policy allows repeated attempts

Only if the adapter contract marks them retry-safe or idempotent.

### Commit retry rule
`commit` MAY be retried automatically only when one of these is true:
- the prior attempt definitively did not execute provider-side;
- the provider supports idempotency and the same idempotency key is reused;
- `status` proves the original commit is incomplete and safe to continue.

Otherwise the effect MUST enter `UNKNOWN` and the transaction MUST enter `RECONCILING`.

### Retry budgets
Each effect MUST carry:
- per-operation retry budget;
- reconciliation polling budget;
- compensation retry budget.

Budget exhaustion MUST escalate to `FAILED_MANUAL_INTERVENTION`.

## `UNKNOWN` handling rules

`UNKNOWN` is a first-class state, not an error string.

### Enter `UNKNOWN` when
- provider commit outcome is ambiguous after timeout;
- process dies after dispatch but before receipt persistence;
- lease is lost during commit and in-flight provider state is uncertain;
- adapter cannot prove whether abort or commit completed.

### On entering `UNKNOWN`
The coordinator MUST:
1. append an `UNKNOWN` transition to the ledger;
2. stop automatic forward progress for that effect;
3. move the transaction to `RECONCILING` unless terminal;
4. attempt `status` resolution before any new commit attempt;
5. preserve the original idempotency key and request metadata.

### Exit `UNKNOWN` only by evidence
An effect may leave `UNKNOWN` only when evidence proves one of:
- commit succeeded -> `COMMITTED`;
- commit did not happen and prepared state can be discarded -> `ABORTED`;
- commit succeeded but later policy requires inverse action -> `COMPENSATING`;
- ambiguity cannot be resolved safely -> transaction `FAILED_MANUAL_INTERVENTION`.

## Recovery and reconciliation rules

### Enter reconciliation when
- the service restarts and finds non-terminal transactions with in-flight effects;
- a transaction lease expires mid-operation;
- any effect is `UNKNOWN`;
- commit or abort flow stopped mid-sequence.

### Reconciliation procedure

1. reacquire coordinator lease;
2. read last durable transaction intent;
3. call `status` on all `COMMITTING` or `UNKNOWN` effects;
4. normalize effect states;
5. choose one path:
   - continue commit;
   - continue abort;
   - begin compensation;
   - escalate to manual intervention.

Reconciliation MUST be deterministic from persisted state plus adapter `status` evidence.

## Manual intervention rules

A transaction MUST enter `FAILED_MANUAL_INTERVENTION` when any of the following are true:

- provider state stays ambiguous past reconciliation budget;
- required locks cannot be reacquired safely;
- compensation fails repeatedly and leaves unacceptable state;
- an irreversible effect was partially executed and policy forbids automatic continuation;
- external drift makes any automatic path unsafe.

When this happens, the system MUST preserve:
- last known effect states;
- all receipts;
- outstanding resource versions;
- recommended operator actions.

## Abort versus compensate

These must never be conflated.

### Abort
Use `ABORTING`/`ABORTED` only before any committed external effect exists.

### Compensate
Use `COMPENSATING`/`COMPENSATED` only after at least one committed effect exists and the system is attempting an inverse action rather than true rollback.

## Concurrency and lease rules

- Every non-terminal transaction MUST have exactly one active coordinator lease.
- Lease loss during `COMMITTING`, `ABORTING`, or `COMPENSATING` MUST force `RECONCILING` on resume.
- A coordinator without a valid lease MUST NOT commit, abort, or compensate.

## Minimal persistence requirements per transition

Every transition record MUST store at least:
- transaction ID;
- effect ID when applicable;
- previous state;
- next state;
- transition reason;
- actor type (`coordinator`, `adapter`, `approver`, `reconciler`);
- timestamp;
- attempt number;
- evidence or receipt reference when applicable.

## Example paths

### Happy path
```text
NEW -> ACTIVE -> VERIFYING -> AWAITING_APPROVAL -> READY_TO_COMMIT -> COMMITTING -> COMMITTED
```

### Verification fail then repair
```text
NEW -> ACTIVE -> VERIFYING -> ACTIVE -> VERIFYING -> AWAITING_APPROVAL -> READY_TO_COMMIT -> COMMITTING -> COMMITTED
```

### Abort before any external commit
```text
NEW -> ACTIVE -> VERIFYING -> ABORTING -> ABORTED
```

### Crash after partial commit
```text
NEW -> ACTIVE -> VERIFYING -> AWAITING_APPROVAL -> READY_TO_COMMIT -> COMMITTING -> RECONCILING -> COMMITTING -> COMMITTED
```
or
```text
NEW -> ACTIVE -> VERIFYING -> AWAITING_APPROVAL -> READY_TO_COMMIT -> COMMITTING -> RECONCILING -> COMPENSATING -> COMPENSATED
```

### Ambiguous provider timeout
```text
Effect: APPROVED -> COMMITTING -> UNKNOWN
Transaction: COMMITTING -> RECONCILING -> FAILED_MANUAL_INTERVENTION
```

## Exit criteria for Step 02

Step 02 is complete when this draft is turned into:
- a final transition spec;
- machine-readable transition tables used by the coordinator;
- conformance tests proving illegal transitions are rejected;
- recovery tests proving `UNKNOWN` always routes through reconciliation.

## Immediate next step after this document

Freeze the approval snapshot contract so `AWAITING_APPROVAL` and `READY_TO_COMMIT` are bound to one exact prepared future.
