# `fdif` command reference

## Starting screen and menu

```bash
fdif
```

Show a non-interactive-safe starting screen with the most useful next commands.
When a current change exists, it also shows how to continue.

```bash
fdif menu
```

Open the numbered interactive menu. This command requires a terminal.

## Home and path configuration

```bash
fdif config
fdif config --explain
```

`config` shows effective values. `config --explain` also identifies the source
of every path.

Unified-home options:

```text
--home PATH        preferred explicit FutureDiff home
--root PATH        alias of --home
FDIF_HOME          environment equivalent
FUTUREDIFF_ROOT    legacy environment equivalent
```

Precedence:

```text
--home / --root > FDIF_HOME > FUTUREDIFF_ROOT > ~/.futurediff
```

Derived defaults:

```text
HOME/current-transaction.json
HOME/futurediff.sock
HOME/runtime
```

Advanced overrides:

```text
--socket PATH             override only the daemon socket
FUTUREDIFF_SOCKET         socket environment override
--state PATH              override only the current-selection file
```

Use `--home` or `FDIF_HOME` when daemon data and safe workspaces must relocate
together. `--state` does not relocate workspaces.

## Start and navigate

```bash
fdif start [repository] [--mode cooperative|enforced]
fdif new [repository] [--mode cooperative|enforced]
fdif workspace [transaction-id]
fdif shell [transaction-id]
fdif status [transaction-id]
fdif transactions
	fdif use --clear
```

`new` is an alias of `start`. Normal `start` output prioritizes the safe path,
the unchanged-source-branch guarantee, and next commands. Add global
`--verbose` to show transaction and mode details.
`use --clear` removes the local current-change selection without touching any
transaction evidence. `use [transaction-id]` validates the ID against the
daemon before saving the selection.

## Review and publish locally

```bash
fdif review [transaction-id] [--full]
fdif seal [transaction-id]
fdif verify [transaction-id] [--policy file.verify.json]
fdif approve [transaction-id] [--yes]
fdif publish [transaction-id] [--yes]
fdif apply [transaction-id] [--yes]
fdif commit [transaction-id] [--yes]
fdif finish [transaction-id] [--yes]
```

`publish` is preferred. `apply` and `commit` are aliases. Publication creates
`futurediff/<transaction-id>` and leaves the current source branch unchanged.

`finish` advances from the transaction's current state:

```text
active    -> seal -> verify -> approve -> publish
sealed    -> verify -> approve -> publish
ready     -> approve or publish, depending on approval state
committed -> report complete
aborted   -> refuse
```

## Optional GitHub publication

```bash
fdif finish [transaction-id] --github [options]
```

Options:

```text
--remote NAME            source remote to inspect; default origin
--base BRANCH            draft-PR base; default captured source branch
--title TEXT             draft-PR title
--body TEXT              draft-PR body
--body-file PATH         read body from a regular file
--github-credential ID   credential selected from daemon configuration
```

Global credential configuration:

```text
--credential-config PATH
--github-credential ID
```

GitHub effects must be selected while the transaction is sealed. Use
`--github` on the first `finish` run. The pull request is always a draft in
this alpha.

See [`FDIF_GITHUB_PUBLICATION.md`](FDIF_GITHUB_PUBLICATION.md).

## Audit and cleanup

```bash
fdif events [transaction-id]
fdif abort [transaction-id] [--yes]
fdif discard [transaction-id] [--yes]
futurediff-audit --root ~/.futurediff
futurediff-audit --root ~/.futurediff --operator-events
```

`fdif events` shows the per-transaction ledger event stream.

`fdif cleanup-lock` removes a proved-stale daemon lock (dead PID, PID reuse, or
previous boot) and its stale socket, records `event_type: lock_cleanup` in the
operator audit trail, and reports `action: cleaned` (`none` when already
cleaned). It refuses — with JSON `action: refused` and exit code 2 — when the
lock is held by a live reachable daemon (`lock_owner_alive`), the owner is
ambiguous (`lock_owner_ambiguous`), permissions are unsafe, the file is
oversized, or `--yes` is missing in non-interactive/`--json` mode
(`confirmation_required`). A corrupt (unparseable) lock with safe `0600`
permissions is eligible for cleanup; the audit write is made durable before any
removal, and each path removal is individually race-safe (flock + inode
verification) — never an atomic pair.

`futurediff-audit` verifies durable local evidence. `--operator-events` verifies the separate tamper-evident operator audit trail for security-sensitive daemon/API actions. `discard` is an alias of `abort`.

## Daemon

```bash
fdif daemon status
fdif daemon start
fdif daemon stop
fdif daemon restart
fdif daemon logs
```

Development-only peer-auth disablement requires both the explicit unsafe flag
and confirmation:

```bash
fdif daemon start --unsafe-disable-peer-auth
```
## Recovery

```bash
fdif recover [transaction-id] [--yes]
```

`recover` reports the recovery state of a change and, with explicit `--yes`,
runs the daemon's canonical recovery endpoint. It never reimplements
reconciliation: the decision stays in the daemon (`Service.Recover`,
`reconcileExternalEffects`). Without `--yes` (or in `--json` mode) it only
reports what is needed.

The JSON report is stable and script-friendly:

```json
{
  "kind": "recovery_report",
  "reason_code": "recovery_required",
  "transaction_id": "tx_...",
  "current_status": "committing",
  "recovery_required": true,
  "safe_to_retry": true,
  "recommended_action": "fdif recover tx_... --yes",
  "workspace_available": true,
  "selection_repaired": false
}
```

Stable `reason_code` values:

```text
no_transactions               no open changes to recover
multiple_transactions         several open changes; select one first
selection_transaction_missing stored selection no longer exists
stale_selection               selection points at a different repository
terminal_selection            the change is already finished
invalid_selection_file        selection file is corrupt or unsafe
workspace_missing             safe working copy is gone
workspace_identity_mismatch   path exists but is not the recorded worktree
recovery_required             canonical recovery should run (needs --yes)
recovery_ambiguous            manual inspection required
daemon_unavailable            daemon is not running
no_recovery_needed            nothing to do; the change is healthy
recovered                     canonical recovery finished
```

Behavior guarantees:

- The guided CLI never silently picks a different change, silently aborts,
  silently recreates a workspace, or deletes evidence.
- The selection pointer is only ever cleared explicitly (`fdif use --clear`)
  or as the reported result of `fdif recover --yes`.
- Risky recovery actions require `--yes`; `--json` mode never prompts.

## System and onboarding

```bash
fdif doctor
fdif demo [--yes] [--keep]
fdif version
fdif completion bash|zsh|fish|powershell
```

`fdif doctor` checks requirements and the effective home, then runs bounded
integrity diagnostics: `ledger_integrity` (a present-but-corrupt ledger reports
`fail`, a missing ledger reports `warn`), `ledger_event_chains` (hash-chain
validation), `daemon_lock` (a corrupt or unsafe lock reports `fail` with the
inspection reason), `storage`, `audit_chain`, and `backup_catalog`.
The demo proves that the current branch stays unchanged and the published safe
branch contains the staged change. It performs no GitHub mutation.

## Global flags

Global flags may appear before or after a subcommand:

```text
--home PATH
--root PATH
--binary PATH
--daemon-binary PATH
--socket PATH
--state PATH
--policy PATH
--credential-config PATH
--github-credential ID
--json
--yes, -y
--verbose, -v
--no-color
--non-interactive
```

Environment variables:

```text
FDIF_HOME
FUTUREDIFF_BINARY
FUTUREDIFF_DAEMON_BINARY
FUTUREDIFF_SOCKET
FUTUREDIFF_ROOT
FUTUREDIFF_CREDENTIAL_CONFIG
FUTUREDIFF_GITHUB_CREDENTIAL_ID
NO_COLOR
FDIF_PLAIN
```
