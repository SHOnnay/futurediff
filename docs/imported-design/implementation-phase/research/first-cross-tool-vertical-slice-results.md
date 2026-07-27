# First Cross-Tool Vertical Slice Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This slice exercises one transaction-shaped preparation flow across:

- repository code staging and verification;
- disposable Postgres migration preview;
- GitHub pull request preparation;
- Slack notification preparation.

It includes:

- one success path;
- one failed-verification path with zero outward side effects.

## Implemented

- `futurediff/integrations/mvpflow/flow.go`
- `futurediff/integrations/mvpflow/flow_test.go`

## Proven behavior

- GitHub PR payloads are prepared with stable preview fingerprints and effect markers.
- Slack notification payloads are prepared with effect markers and explicit best-effort support level.
- Postgres migration preview runs in a disposable database and captures rollback-verified evidence.
- Repository changes are staged in a worktree and remain out of the source repo before commit.
- Passing staged verification produces an `AWAITING_APPROVAL` transaction with an inspectable patch.
- Failing staged verification aborts the transaction while preserving patch and verification evidence.
- The failed-verification path performs zero GitHub and Slack network calls.

## Verification

- `go test ./...` passes, including `integrations/mvpflow`.
- The integration tests cover:
  - successful cross-tool preparation;
  - failed verification with zero outward network side effects.

## Why this matters

This is the first end-to-end proof that FutureDiff can coordinate multiple effect domains in one preparation flow while keeping the source repo unchanged, preserving evidence, and failing closed before outward effects when verification goes bad.

## Next useful move

Drive this prepared cross-tool slice through more coordinator-owned transition and approval state so the flow stops depending on bootstrap-only orchestration helpers.
