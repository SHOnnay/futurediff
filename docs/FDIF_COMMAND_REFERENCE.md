# `fdif` command reference

## Home

```bash
fdif
```

Open the numbered guided menu.

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

`new` is an alias of `start`. `workspace` prints the isolated working-copy path.
`shell` opens the user's normal shell there.

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

`publish` is preferred. `apply` and `commit` are aliases. The command creates
`futurediff/<transaction-id>` and leaves the current source branch unchanged.

`finish` advances from the transaction's current state:

```text
active    → seal → verify → approve → publish
sealed    → verify → approve → publish
ready     → approve or publish, depending on approval state
committed → report complete
aborted   → refuse
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
--body-file PATH         read draft-PR body from a regular file
--github-credential ID   credential ID selected from daemon configuration
```

Global credential configuration:

```text
--credential-config PATH
--github-credential ID
```

Environment equivalents:

```text
FUTUREDIFF_CREDENTIAL_CONFIG
FUTUREDIFF_GITHUB_CREDENTIAL_ID
FUTUREDIFF_GITHUB_TOKEN
```

The token environment name is determined by the selected credential's source
configuration. `FUTUREDIFF_GITHUB_TOKEN` is the repository example.

GitHub effects must be selected while the transaction is sealed. Use
`--github` on the first `finish` run. The final confirmation word is `SEND`.
The pull request is always created as a draft in this alpha.

See [`FDIF_GITHUB_PUBLICATION.md`](FDIF_GITHUB_PUBLICATION.md).

## Audit and cleanup

```bash
fdif events [transaction-id]
fdif abort [transaction-id] [--yes]
fdif discard [transaction-id] [--yes]
```

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
fdif config
fdif demo [--yes] [--keep]
fdif version
fdif completion bash|zsh|fish|powershell
```

The demo proves both local boundaries: the current branch remains unchanged,
and the published FutureDiff branch contains the staged change. It does not
perform a GitHub provider mutation.

## Global flags

Global flags may appear before or after a subcommand:

```text
--binary PATH
--daemon-binary PATH
--socket PATH
--state PATH
--policy PATH
--credential-config PATH
--github-credential ID
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
FUTUREDIFF_CREDENTIAL_CONFIG
FUTUREDIFF_GITHUB_CREDENTIAL_ID
NO_COLOR
FDIF_PLAIN
```
