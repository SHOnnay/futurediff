# Recovery and Idempotency Design Note

## Status

Initial research-phase note.

## Purpose

This note sharpens retry, idempotency, reconciliation, and manual-intervention behavior for the MVP. It translates the state-machine rules into practical coordinator behavior.

## Core decisions

## 1. Idempotency keys are effect-scoped

Use this shape:

```text
idempotency_key = transaction_id + effect_id + commit_attempt_scope
```

Rules:
- one effect gets one stable idempotency identity;
- retries for the same logical commit reuse the same key;
- repaired or materially changed effects get a new effect ID and therefore a new key.

## 2. Blind retry is prohibited after ambiguous commit

After any ambiguous provider outcome:
- do not call `commit` again immediately;
- transition effect to `UNKNOWN`;
- move transaction to `RECONCILING`;
- attempt `status` or equivalent evidence gathering first.

This is non-negotiable. Blind retry is how duplicate external effects happen.

## 3. Idempotency strength differs by adapter

### Strong internal idempotency
Applies to:
- git/filesystem promotion;
- staged artifact promotion;
- internal ledger writes;
- approval snapshot creation.

These can be strongly deduplicated by FutureDiff itself.

### Medium idempotency
Applies to:
- Postgres migrations when migration identity and transaction records are strong;
- GitHub PR creation/update when search by head/base and recorded receipts are used.

These are recoverable, but not truly provider-native idempotent in the strongest sense.

### Weak/best-effort idempotency
Applies to:
- Slack posting.

FutureDiff can reduce duplicates through outbox discipline, metadata, and receipt reconciliation, but it should not pretend this is as strong as Stripe-style idempotency.

## 4. `UNKNOWN` exits only by evidence

An effect may leave `UNKNOWN` only when evidence proves one of:
- the original commit succeeded;
- the original commit did not happen and the effect can safely abort;
- the original commit succeeded and compensation is required;
- the result cannot be resolved safely and needs manual intervention.

Evidence sources:
- adapter `status`;
- provider receipt lookup;
- durable outbox record;
- stored response/receipt IDs;
- resource search using metadata markers;
- manual operator inspection.

## 5. Reconciliation must be bounded

Each effect needs:
- status poll budget;
- commit retry budget where safe;
- compensation retry budget.

When the budget is exhausted:
- stop pretending it is still automatic;
- transition toward `FAILED_MANUAL_INTERVENTION`.

## Practical retry rules by adapter type

## Git/filesystem

### Safe retry
- yes for prepare/preview/abort;
- yes for commit when commit target is exact staged patch promotion and local state proves prior attempt did not finish.

### Unknown triggers
- process crash during patch promotion;
- partial local promotion with missing final receipt.

### Recovery approach
- inspect target repo state;
- compare committed tree/patch markers;
- finish or abort deterministically.

## Runtime/container

### Safe retry
- yes for prepare/preview/abort;
- no blind rerun of raw commands to simulate exact commit.

### Unknown triggers
- container crash after artifact generation but before artifact sealing;
- incomplete artifact/receipt persistence.

### Recovery approach
- treat staged artifact bundle as the truth object;
- if bundle integrity is unclear, re-stage instead of pretending commit is exact.

## Postgres

### Safe retry
- status/freshness checks yes;
- commit retry only when it is provable the prior COMMIT did not take effect or migration system semantics make re-run safe.

### Unknown triggers
- connection loss during COMMIT;
- migration runner crash after DDL/DML but before receipt write;
- ambiguous state in non-fully-transactional migration flows.

### Recovery approach
- inspect migration table / schema version / transactional side effects;
- if outcome remains unclear, escalate rather than replay blindly.

## GitHub

### Safe retry
- search/status checks yes;
- create/update retry only after checking whether the intended PR already exists or whether the original operation definitively failed.

### Unknown triggers
- timeout after PR create/update request;
- network loss after acceptance;
- race with branch changes.

### Recovery approach
- search by head/base and recorded payload markers;
- compare returned PR state with prepared preview;
- if found, bind receipt and mark committed;
- if not provable, remain in reconciliation/manual path.

## Slack

### Safe retry
- outbox and status-search checks yes;
- message send retry only after checking for prior delivered message using stored refs or metadata search when possible.

### Unknown triggers
- timeout after `chat.postMessage`;
- rate-limit/retry ambiguity;
- receipt loss after successful post.

### Recovery approach
- inspect returned IDs when available;
- search target channel/thread using metadata and timestamp window when possible;
- if duplicate risk remains high, escalate instead of blind resend.

## Coordinator rules implied by this note

## Rule A — receipt first
Whenever possible, persist an intent/attempt record before the provider call and a receipt record immediately after success.

## Rule B — status before retry
For any ambiguous external effect, `status` or equivalent lookup happens before a second `commit` attempt.

## Rule C — effect changes mean new identity
If the payload, prepared fingerprint, target resource set, or commit mode changes materially, create a new effect ID instead of reusing the old one.

## Rule D — compensation is not fallback retry
If one effect has already committed and the remaining path becomes unsafe, choose between:
- forward completion; or
- compensation.

Do not smuggle compensation behavior into ordinary retry logic.

## Rule E — provider weakness must stay visible
When an adapter has weaker idempotency semantics, the coordinator must keep that visible in:
- support level;
- benchmark expectations;
- manual-intervention thresholds.

## Manual-intervention threshold guidance

Escalate faster when:
- the provider lacks strong idempotency;
- the effect is externally visible;
- duplicate side effects are socially or operationally expensive;
- the resource search space is too fuzzy.

This is why Slack and some GitHub flows should reach manual review faster than local git promotion.

## Recommended first implementation spikes

1. **Postgres lease/recovery spike**
   - prove a worker can recover a transaction after losing lease mid-commit.

2. **GitHub duplicate-detection spike**
   - prove a create-PR timeout can be resolved by searching existing PRs safely enough for MVP.

3. **Slack ambiguous-send spike**
   - prove whether metadata plus channel/thread search is good enough to avoid unsafe blind resend.

4. **Local promotion spike**
   - prove exact patch promotion can recover cleanly after crash.

## Bottom-line decisions

- strong idempotency claims belong mostly to local/staged resources;
- external providers require evidence-driven reconciliation;
- `UNKNOWN` is normal, not embarrassing;
- manual intervention is a valid terminal path for weak or ambiguous providers.
