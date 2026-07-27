# MVP Adapter Reality Matrix

## Status

Initial research-phase matrix.

## Purpose

This matrix forces honest support-level decisions for the MVP adapters. It is the line between real transactional guarantees and wishful product language.

Support levels use the EffectSpec 0.1 terms:
- `exact_prepare_commit`
- `preview_with_freshness_check`
- `idempotent_best_effort`
- `unsupported`

## Summary matrix

| Adapter | prepare | preview | exact commit | freshness check | idempotency | compensation | unknown triggers | support level | main risk |
|---|---|---|---|---|---|---|---|---|---|
| Git/filesystem | Yes, via worktree/snapshot/staged patch | Yes, exact diff | Yes, by applying exact staged patch/artifact without rerunning agent | target branch HEAD / repo state / file conflicts | strong, transaction-scoped internal IDs | revert patch or discard uncommitted worktree | crash during patch apply or ambiguous local promotion failure | `exact_prepare_commit` | conflict with external repo drift before promotion |
| Runtime/container | Yes, via isolated staged runtime | Partial: command plan + resulting artifacts/logs, not perfect future output | Only for promoted artifacts; never by rerunning commands | container image hash / env fingerprint / dependency lock state | strong for staged artifact promotion, weak for raw command replay | discard staged container/artifacts | process crash during artifact capture or incomplete staged run evidence | `exact_prepare_commit` for artifact promotion only | people may treat raw shell replay as exact commit when it is not |
| Postgres | Yes, in disposable DB or transactional staging where possible | Yes, schema diff / migration plan / rollback test result | No exact prod commit against prepared provider-side version in general | schema version / migration watermark / lock risk / target DB state | moderate if migrations are uniquely identified and transaction IDs are persisted | down migration when valid, or restore/snapshot workflow where available | timeout during commit, partial non-transactional migration behavior, connection loss | `preview_with_freshness_check` | production DB may diverge from disposable preview environment |
| GitHub PR create/update | Yes, locally as payload draft | Yes, exact request payload + branch/PR diff summary | No exact prepared server-side commit token | base branch SHA / head branch SHA / existing open PR by head+base | best-effort dedupe by searching head/base/title/body markers | close PR or update PR body/branch as follow-up | create request timeout, network loss after server accepted PR, concurrent branch changes | `preview_with_freshness_check` | no native prepare or generic idempotency key |
| Slack message outbox | Yes, durable outbox payload | Yes, exact payload preview | Exact prepared payload can be released, but provider rendering may still vary | channel/thread existence, auth scope, reply target existence | best-effort via durable outbox + metadata/search, not strong provider idempotency | delete message when allowed, or compensating follow-up message | timeout after send, rate-limit/retry ambiguity, provider-side formatting mutation | `idempotent_best_effort` | no strong native idempotency guarantee for postMessage |

## Detailed notes by adapter

## 1. Git/filesystem

### What is real
- Git worktrees give FutureDiff a strong reversible local boundary.
- Exact file diffs are previewable.
- Commit can promote the exact staged patch without rerunning the LLM.

### What FutureDiff should claim
- strongest MVP adapter;
- exact prepared-version approval is realistic;
- great benchmark anchor for “no second agent run”.

### What it should not claim
- external repo state cannot be ignored at promotion time;
- branch drift and merge conflict checks still matter.

## 2. Runtime/container

### What is real
- Containerized execution is a good staging boundary.
- Resulting artifacts, logs, test results, and generated patches are previewable.
- The safe commit object is the **resulting artifact set**, not “rerun the same commands later”.

### Decision
Treat runtime/container as exact only when the commit step is artifact promotion from staged output.

### What FutureDiff should block
- raw shell actions that must be re-executed outside staging as if they were exact prepared commits.

## 3. Postgres

### What is real
- PostgreSQL supports strong transactional semantics inside one database transaction.
- Disposable DB preview, schema diff, and rollback testing are useful and honest.
- Many migrations can be validated before prod commit.

### What remains weaker
- preview in disposable DB is not identical to commit against live production state;
- locking behavior, data size, extensions, and concurrent workload can differ.

### MVP claim
Use `preview_with_freshness_check`, not exact prepare/commit, for production-facing DB changes.

### Freshness requirements
- current schema version / migration table;
- lock-risk heuristics;
- target DB reachability;
- optional drift check on dependent tables/indexes.

## 4. GitHub PR create/update

### What is real
- FutureDiff can preview the exact request payload and repository diff.
- FutureDiff can freshness-check base/head SHA before commit.
- FutureDiff can search for existing open PRs on the same head/base as a best-effort duplicate guard.

### What remains weaker
- GitHub does not provide a generic prepare/commit token for PR creation;
- native idempotency is not the core model here;
- accepted request plus lost response can create ambiguity.

### MVP claim
Use `preview_with_freshness_check`.

### Compensation
- close mistakenly created PR;
- update title/body/labels if correction is enough.

## 5. Slack message outbox

### What is real
- FutureDiff can render and store the exact outbound payload before release.
- A durable outbox is the correct commit boundary.
- Slack payload preview is strong from the gateway perspective.

### What remains weaker
- provider-rendered message behavior can vary;
- `chat.postMessage` does not offer strong generic provider idempotency in the same shape as Stripe-style APIs;
- ambiguous send after timeout is a real risk.

### MVP claim
Use `idempotent_best_effort`, not exact strong commit.

### Risk reduction tactics
- include transaction/effect IDs in message metadata where safe;
- persist returned message/channel/thread IDs as receipts;
- perform status/search reconciliation before retrying ambiguous sends.

## Honest product-boundary conclusions

### Strongest MVP domains
- Git/filesystem
- staged artifact promotion

### Medium-strength MVP domains
- Postgres migrations with freshness checks
- GitHub PR flows with head/base drift checks

### Weakest MVP domain
- Slack posting due to weaker native idempotency semantics

## What this means for benchmark messaging

FutureDiff should lead with:
- local repo changes prevented from causing external damage;
- verified DB/GitHub/Slack transaction abort with zero real side effects;
- crash recovery and duplicate prevention where the adapter support level actually justifies the claim.

FutureDiff should not overclaim:
- perfect provider-wide rollback;
- generic exact commit semantics for GitHub or Slack;
- irreversible safety guarantees outside declared support levels.

## Resulting next research item

Use these support-level decisions to write the recovery/idempotency note so retry, `UNKNOWN`, and compensation behavior are sharp per adapter rather than generic.
