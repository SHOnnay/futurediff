# Priority 2 Spike Results

## Status

Implemented and verified in the bootstrap repo.

## Covered so far

- S-004 disposable Postgres migration preview
- S-005 GitHub duplicate-recovery
- S-006 Slack ambiguous-send handling

## S-004 — Disposable Postgres migration preview

### Implemented
- `staging/postgrespreview/preview.go`
- `staging/postgrespreview/preview_test.go`
- `internal/testpostgres/testpostgres.go`
- `control-plane/locks/postgreslease/store_test.go` updated to reuse the shared disposable Postgres harness

### Proven behavior
- a disposable PostgreSQL 18 instance is created on demand;
- migration `up` SQL runs inside the disposable instance;
- schema dump before migration is captured;
- schema dump after migration is captured;
- schema diff is written to evidence;
- migration `down` SQL runs inside the disposable instance;
- rollback verification confirms the normalized schema returns to the pre-migration state;
- the result is explicitly marked `preview_with_freshness_check`, not exact commit.

### Verification
- Go tests pass.
- a live migration preview scenario was executed and returned:
  - `support=preview_with_freshness_check`
  - `commit_mode=freshness_check_required`
  - `rollback_verified=true`

## S-005 — GitHub duplicate-recovery

### Implemented
- `adapters/github/prcreate/adapter.go`
- `adapters/github/prcreate/adapter_test.go`

### Proven behavior
- PR creation can be prepared with a stable preview fingerprint;
- the adapter is explicitly marked `preview_with_freshness_check`;
- an ambiguous create timeout can be recovered by searching open PRs on the same head/base and matching the FutureDiff effect marker;
- recovery avoids blind duplicate PR creation.

### Verification
- Go tests pass against an `httptest` GitHub stub.
- the timeout-and-recover scenario proves only one create call and one stored PR occur.

## S-006 — Slack ambiguous-send handling

### Implemented
- `adapters/slack/outbox/adapter.go`
- `adapters/slack/outbox/adapter_test.go`

### Proven behavior
- Slack payloads are prepared with a stable fingerprint and effect marker metadata;
- the adapter is explicitly marked `idempotent_best_effort`;
- an ambiguous send timeout can be recovered by checking channel history for the stored metadata marker;
- recovery avoids blind duplicate send attempts.

### Verification
- Go tests pass against an `httptest` Slack stub.
- the timeout-and-recover scenario proves only one send call and one stored message occur.

## What remains open after Priority 2

- first failed verification path

## Current verdict

Priority 2 is now effectively complete for the current bootstrap scope: database preview, GitHub duplicate-recovery, and Slack ambiguous-send handling all have verified spike implementations.

The MVP now has a real database preview path with evidence capture and rollback verification, while keeping the honest contract that production-facing database commit remains freshness-check driven.
