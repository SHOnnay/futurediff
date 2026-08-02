# ADR-098: Guided recovery and stale-selection hardening for `fdif`

- Status: accepted for public alpha
- Scope: guided CLI (`internal/guidedcli`), selection store, error contract

## Context

`fdif` is the guided human entry point over the canonical `futurediffd` daemon
and `futurediff` low-level client (ADR-096). It keeps a single client-side
selection pointer (`current-transaction.json`) naming the transaction the user
is currently working on. Several failure classes are not yet handled
explicitly:

1. The selection pointer can outlive the transaction it names
   (squash-merged/aborted/expired/committed work, home recreated, transaction
   ledger replaced).
2. The selection file itself can be malformed, oversized, tampered with, or
   replaced between validation and use.
3. The safe workspace (`git worktree` copy under the runtime root) can be
   deleted, replaced by a symlink, or replaced by a different repository while
   a transaction is still open.
4. A transaction can be left in `committing` or `needs_reconciliation` after an
   interrupted publish; today only the low-level `futurediff recover <id>`
   handles that, and it is not wired into the guided flow.
5. The canonical daemon owns the authoritative state machine
   (`internal/domain/state.go`), the staging manager
   (`internal/staging/git.go`), effect reconciliation
   (`internal/app/external_effects.go`), and the operator audit trail. Any
   guided recovery must invoke those canonical paths, never reimplement them.

The current guided flow silently clears a stale pointer in
`resolveTransaction` when the daemon cannot find the selected transaction and
then silently picks the sole remaining eligible transaction. For a *risky*
command (`publish`/`finish`/`abort`) that is a fail-open behavior: the user
asked to operate on one change and the CLI may silently choose another. This
ADR closes that gap with an explicit, fail-closed guided recovery contract.

## Decisions

### D1. Add a guided `fdif recover [transaction-id] [--yes]` command

`fdif recover` is a thin, strictly-reporting layer over the canonical
`POST /v1/transactions/{id}/recover` endpoint. It never implements a recovery
decision itself; it classifies the situation, prints the reason code and a
recommended action, and only when the canonical state is
`committing`/`needs_reconciliation` (the only recoverable states) invokes the
canonical `recover` via the existing `Engine` interface after explicit
confirmation.

The command must handle at least these situations:

- no selection file and multiple eligible transactions -> `multiple_transactions`
- no selection file and one eligible transaction -> report it, do not silently
  choose it for a risky recovery
- no selection file and zero transactions -> `no_transactions`
- selection file valid but transaction missing from daemon -> `selection_transaction_missing`
- selection file valid, transaction present, but repository root differs from
  the canonical workspace repository -> `stale_selection`
- selection file points at a terminal transaction (committed/aborted/
  compensated/manual_intervention) -> `terminal_selection`
- selection file unreadable/malformed/unsafe -> `invalid_selection_file`
- workspace path missing -> `workspace_missing`
- workspace path replaced by symlink/non-directory -> `workspace_identity_mismatch`
- workspace path is a different git worktree -> `workspace_identity_mismatch`
- transaction in `committing`/`needs_reconciliation` -> `recovery_required`
- daemon unreachable -> `daemon_unavailable`
- ambiguous reconciliation outcome (manual_intervention) -> `recovery_ambiguous`

Explicit `--yes` is required for the risky canonical recovery path and for any
selection-pointer repair (clearing or replacing the pointer). JSON mode never
prompts; it fails closed with a stable reason code when `--yes` is absent.

### D2. Never silently mutate a selection pointer

`fdif use` gains `--clear` as the only way to explicitly clear the pointer.
`resolveTransaction` stops clearing the pointer and silently re-selecting for
risky commands; instead, risky commands surface the stale/missing state with a
reason code and recommended action. Read-only commands (`status`, `workspace`,
`review`) may keep lenient behavior but must not silently repair the pointer
either — they report the stale selection and recommend `fdif use`.

### D3. Harden the selection store

`StateStore` (`internal/guidedcli/state.go`) gains:

- bounded file size (reject files above a fixed cap, currently 64 KiB);
- strict JSON decoding that rejects unknown fields and trailing garbage;
- validation that `transaction_id` is present, non-empty, and matches the
  `tx_` identifier shape used by the ledger;
- validation that `repository_root` (when non-empty) is an absolute path;
- validation that `selected_at` parses and is not absurdly in the future;
- keep existing symlink, non-regular-file, POSIX permission (0o077) and
  canonical-parent-path rejections;
- atomic replacement on save (temp file + rename) with durability sync;
- explicit concurrent read/write/clear behavior: `Load` either returns the
  last fully-written value or an error; `Clear` and `Save` are atomic so
  readers never observe a partial file;
- close the validation/read TOCTOU: `Load` validates the file it *actually
  opened* (fd-based `fstat`), not a path re-looked-up after validation.

### D4. Workspace classification by transaction stage

`recover` and `status` classify the workspace directory on disk and pair that
with the transaction state:

| Transaction state      | Workspace missing                | Workspace identity mismatch   |
|------------------------|----------------------------------|-------------------------------|
| `active`/`sealed`/`verifying`/`ready`/`stale`/`failed_verification` | `workspace_missing`; sealed material is durable in the ledger, edits since seal are not; guided recovery can recreate nothing — the user must `abort` or restore | `workspace_identity_mismatch`; fail closed, never operate on a stranger's directory |
| `committing`/`needs_reconciliation` | `workspace_missing` is expected mid-recovery; canonical `recover` reconciles from the ledger and integration ref | n/a — canonical path governs |
| terminal states        | report terminal state; missing workspace does not make a committed transaction un-committed | n/a |

Rule: the guided CLI never recreates a workspace and never writes into a path
it did not create. Workspace recreation is out of scope for this milestone; the
contract documents it as unavailable and recommends `fdif abort` (explicit,
`--yes`) as the safe terminal cleanup for a lost active workspace.

### D5. Stable error contract

Every recovery/selection outcome emits stable JSON fields when `--json` is
requested and a stable human line otherwise:

```json
{
  "kind": "recovery_report",
  "reason_code": "workspace_missing",
  "transaction_id": "tx_...",
  "current_status": "active",
  "recovery_required": false,
  "safe_to_retry": false,
  "recommended_action": "fdif abort tx_... --yes",
  "workspace_available": false,
  "selection_repaired": false
}
```

Reason codes are exactly the strings in D1. `safe_to_retry` is true only when
repeating the command is idempotent (e.g. `workspace_missing` on a terminal
transaction). Risky recovery always requires `--yes`; JSON mode without
`--yes` returns the reason code and refuses.

### D6. Recovery always goes through the canonical daemon

The guided `recover` invokes `Engine.Run(ctx, "recover", id)` which performs
`POST /v1/transactions/{id}/recover`. That endpoint acquires a lease, inspects
the integration ref, reconciles external effects through
`reconcileExternalEffects`, and records `transaction.recover.*` operator-audit
events. The guided layer only formats the canonical `TransactionView`
response. The exactly-once, no-duplicate-push, no-force-push guarantees already
proven by the GitHub write-recovery certification hold unchanged.

## Consequences

- `fdif recover` becomes the single guided entry point for interrupted publish
  flows; low-level `futurediff recover` remains the scriptable primitive.
- Risky commands can no longer silently operate on a different transaction
  than the one selected.
- Selection store failures become explicit and actionable instead of being
  silently cleared.
- No new daemon API, no state-machine changes, no changes to effect
  reconciliation; the daemon is untouched.
- Windows behavior is unchanged and unclaimed.
