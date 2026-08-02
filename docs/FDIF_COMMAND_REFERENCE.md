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
fdif use [transaction-id]
```

`new` is an alias of `start`. Normal `start` output prioritizes the safe path,
the unchanged-source-branch guarantee, and next commands. Add global
`--verbose` to show transaction and mode details.

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

`futurediff-audit` verifies durable local evidence. `--operator-events` verifies the separate tamper-evident operator audit trail for security-sensitive daemon/API actions.

`discard` is an alias of `abort`.
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

## System and onboarding

```bash
fdif doctor
fdif demo [--yes] [--keep]
fdif version
fdif completion bash|zsh|fish|powershell
```

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
