# `fdif` command reference

## Home

```bash
fdif
```

Open the numbered guided menu.

## Start and navigate

```bash
fdif start [repository] [--mode cooperative|enforced]
fdif workspace [transaction-id]
fdif shell [transaction-id]
fdif status [transaction-id]
fdif transactions
fdif use [transaction-id]
```

`workspace` prints the isolated workspace path. `shell` opens the user's normal shell there.

## Review and publish

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

`publish` is preferred. `apply` and `commit` are aliases. The command creates `futurediff/<transaction-id>` and leaves the current source branch unchanged.

`finish` advances from the transaction's current state:

```text
active    → seal → verify → approve → publish
sealed    → verify → approve → publish
ready     → approve or publish, depending on approval state
committed → report complete
aborted   → refuse
```

## Audit and cleanup

```bash
fdif events [transaction-id]
fdif abort [transaction-id] [--yes]
```

## Daemon

```bash
fdif daemon status
fdif daemon start
fdif daemon stop
fdif daemon restart
fdif daemon logs
```

Development-only peer-auth disablement requires both the explicit unsafe flag and confirmation:

```bash
fdif daemon start --unsafe-disable-peer-auth
```

## System and onboarding

```bash
fdif doctor
fdif config
fdif demo [--yes] [--keep]
fdif version
fdif completion bash|zsh|fish|powershell
```

The demo proves both boundaries: the current branch remains unchanged, and the published FutureDiff branch contains the staged change.

## Global flags

Global flags may appear before or after a subcommand:

```text
--binary PATH
--daemon-binary PATH
--socket PATH
--state PATH
--policy PATH
--json
--yes, -y
--no-color
--non-interactive
```

Environment variables:

```text
FUTUREDIFF_BINARY
FUTUREDIFF_DAEMON_BINARY
FUTUREDIFF_SOCKET
FUTUREDIFF_ROOT
NO_COLOR
FDIF_PLAIN
```
