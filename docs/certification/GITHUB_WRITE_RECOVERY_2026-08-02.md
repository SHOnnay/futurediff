# Real GitHub write-and-recovery certification — 2026-08-02

This is real external evidence that FutureDiff can safely perform controlled
GitHub write operations and recover from interruption. It was executed on a
disposable repository owned by the currently authenticated account, using
FutureDiff's own supported guided workflow (`fdif finish --github`), not manual
Git commands as a substitute.

## Certification summary

| Field | Value |
|---|---|
| Certification date | 2026-08-02 |
| Disposable repository | `SHOnnay/futurediff-certification-20260802143944-25328` |
| Disposable repository URL | https://github.com/SHOnnay/futurediff-certification-20260802143944-25328 |
| Repository created at | 2026-08-02T08:39:45Z |
| Repository final state | archived (deletion requires the `delete_repo` token scope, which the certification token does not carry; archive satisfies the delete-or-archive cleanup requirement) |
| Workflow used | `fdif start` → workspace edit → `fdif finish --github` (seal, prepare GitHub effects, verify, approve, local safe branch, push, draft PR) |
| Success transaction | `tx_6bffcae6e24d8c82c99e5001` |
| Success safe branch | `futurediff/tx_6bffcae6e24d8c82c99e5001` |
| Success commit SHA | `ca7945b7ac4f8dff082e6212f76e66901c41473d` |
| Success PR | #1 (draft, open then closed during cleanup) |
| Recovery transaction | `tx_3fedb9aa401c00cce4cf35d8` |
| Recovery safe branch | `futurediff/tx_3fedb9aa401c00cce4cf35d8` |
| Recovery PR | #2 (draft, open then closed during cleanup) |
| Default branch | `main` at `0072c65a221768194be89a9b37e19c38c54220e0` before and after all operations (never mutated) |
| Automatic merge | never enabled; no merge occurred |
| Force push | none |
| Windows support | not claimed |

## Real operations performed

1. **Repository admission**: `fdif start` on the disposable clone admitted an
   ordinary repository under the stable-default repository-admission policy
   `stable-default-v0.2`.
2. **Safe mutation**: a file was modified inside the FutureDiff safe working
   copy; the source repository worktree remained clean and unchanged.
3. **Exact-version approval**: the transaction was sealed, GitHub branch and
   draft-PR effects were prepared while sealed, the change was verified,
   approved, and committed only after the final operator decision (`--yes`).
4. **Local safe branch**: `futurediff/<transaction-id>` was materialized
   locally; the current branch was not modified.
5. **GitHub push**: the create-only branch was pushed with an absent-ref lease
   (`--force-with-lease=<ref>:` against a branch that must not exist).
6. **Draft PR creation**: a draft pull request was created against `main`.
7. **Transaction verification**: `futurediff` returned a durable receipt
   (`github://…/refs/heads/…` and `github://…/pulls/N`).
8. **Operator audit recording and verification**: every high-risk API call
   (effect preparation, verify, approve, commit) was recorded in the local
   tamper-evident operator trail; `futurediff-audit -operator-events` verified
   the hash chain (`valid: true`).
9. **Remote state confirmation**: `gh api` confirmed the branch SHA, the draft
   PR, a single FutureDiff commit per PR, and an unchanged default branch.

## Denial cases tested (all denied before mutation)

| Case | Result | Detail |
|---|---|---|
| Publish without approval | denied | `change passed checks but is not approved; run fdif approve` |
| Dirty worktree | denied | `repository is dirty; use stage_from_head explicitly` |
| Detached HEAD | denied | `has a detached HEAD; checkout a branch before running fdif start` |
| Shallow repository | denied | `repository admission rejected by stable-default-v0.2: shallow_repository_not_allowed` |
| Direct default-branch mutation | denied | branch adapter: `branch must be a safe futurediff/* branch` |
| Unknown credential | denied | `credential access denied: credential is unknown or disabled` |
| Empty-patch commit | denied | `No valid patches in input` |
| Duplicate operation | no duplicate | re-running finish returned the same PR and commit; remote branch/PR counts unchanged |

Every denial was audited in the operator trail; none created a commit, push,
PR, or default-branch mutation.

## Recovery scenario

A controlled incomplete transaction was created: GitHub effects were prepared
while the transaction was sealed, and the daemon was interrupted before commit
(one variant produced `needs_reconciliation`, which `futurediff recover`
resolved to `ready` with the message `all unresolved effects proved absent or
remain prepared`). Recovery demonstrated:

- incomplete-state detection (`needs_reconciliation` / `ready` with prepared effects);
- operator-facing recovery guidance (`run fdif status … and futurediff recover …`);
- safe resume: recovery then low-level commit with the approved
  `transaction_digest` executed the prepared effects exactly once;
- no duplicate commit, push, or PR (1 FutureDiff commit per PR, verified via `gh api`);
- consistent final local state (`committed`) and GitHub state (exactly one
  branch and one PR for the transaction);
- valid operator-audit hash chain after recovery (`valid: true`, 126 records);
- repeated recovery is safe (recover on a non-recoverable state refuses).

### Exact recovery guarantee

FutureDiff supports **resume/retry and abort** of incomplete transactions. It
does **not** claim full rollback of an already-committed external effect.
Incomplete transactions are detected, reconciled to a safe state (effects
proved absent → `ready`; effects committed → finalized), and retried exactly
once via idempotency keys and durable receipts.

## Cleanup result

- Both disposable PRs closed (#1, #2).
- Both temporary `futurediff/*` feature branches deleted.
- Disposable repository archived after evidence verification.
- No unrelated repository was modified.
- No secrets were committed; no credential files were created in the repository.
- Local clone and shallow test clone removed.

## Redaction statement

No tokens, cookies, authorization headers, environment dumps, or
credential-helper files were captured. The operator audit trail and all
evidence files contain no secret material (verified by pattern scan).

## Audit verification

`futurediff-audit -operator-events` reports `valid: true` for the full local
operator audit trail at the end of certification (126 records, hash chaining
intact).

## Limitations

- Certification covers the GitHub write surface exercised by the guided
  `fdif finish --github` flow (branch push and draft PR) plus recovery.
- The disposable repository was archived rather than deleted because the
  certification token does not carry the `delete_repo` scope; this does not
  affect the evidence.
- The operator audit trail is tamper-evident, not tamper-proof against a fully
  privileged host administrator.
- Windows runtime and installer support remain unclaimed.

## Evidence files

See `docs/certification/github-write-recovery-20260802/`:

- `00-success-path.json` — success-path transaction, branch, commit, PR, audit facts;
- `01-finish.json` — captured `fdif finish --github` JSON output;
- `02-denial-paths.json` — denial cases and outcomes;
- `03-recovery-interrupted-finish.json` — interrupted finish output;
- `04-recovery-commit.json` — recovery commit receipts;
- `05-recovery-path.json` — recovery scenario and guarantee;
- `06-cleanup.json` — cleanup result.
