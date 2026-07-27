# FutureDiff Task 081 — Safe abandoned-transaction expiry

## Objective

Prevent indefinitely abandoned pre-commit transactions from consuming worktrees and operator attention without introducing an automatic path through unsafe in-flight states.

## Delivered

- `internal/transactionexpiry` policy, planner, and apply engine.
- `futurediff-expire` command.
- Versioned policy schema and example.
- Offline exclusive-lock requirement for apply.
- Dry-run default and exact confirmation: `EXPIRE_STALE_FUTUREDIFF_TRANSACTIONS`.
- Safe-state allowlist: `active`, `sealed`, `failed_verification`, `ready`, `stale`.
- Revalidation immediately before apply.
- Normal FutureDiff abort flow, including prepared-effect abort and worktree cleanup.
- Durable `transaction_expiry_actions` record.

## Safety properties

The policy cannot select `verifying`, `committing`, `compensating`, `needs_reconciliation`, `manual_intervention`, or terminal transactions. Candidate count can be bounded. Apply fails if the transaction changed after planning.

## Validation

Unit and race tests covered safe/unsafe state validation, wrong-confirmation rejection, successful abort, terminal status, and action recording. A live daemon-created active transaction was aged in a disposable ledger and successfully expired after daemon shutdown.
