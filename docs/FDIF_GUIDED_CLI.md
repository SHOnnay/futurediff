# `fdif`: the guided FutureDiff CLI

`fdif` is the human-facing entry point for FutureDiff:

```text
futurediffd   authoritative local daemon
futurediff    exact, scriptable low-level client
fdif          newcomer-oriented guided command
```

The public-alpha workflow is local-first and cooperative. FutureDiff creates a
safe Git working copy. A human or an externally launched coding agent edits
that copy. FutureDiff then reviews, verifies, approves, and publishes the exact
result as a new safe branch.

FutureDiff does **not** launch or supervise coding agents in this alpha.

## First run

Running `fdif` without a subcommand shows a deterministic starting screen:

```text
FutureDiff
Review AI-assisted changes before they reach GitHub or your current branch.

Start a change:
  fdif start

Check your setup:
  fdif doctor

Try a disposable demo:
  fdif demo --yes
```

This behavior is the same when stdout is not a terminal. It does not fail with
“no command supplied,” and it does not silently require an interactive menu.

The numbered menu remains available explicitly:

```bash
fdif menu
```

`fdif menu` requires an interactive terminal.

When a current change is selected, the starting screen points to `status`,
`review --full`, and `finish`. Transaction IDs stay hidden unless verbose or
structured output is requested.

## One home, consistent paths

The default FutureDiff home is:

```text
~/.futurediff
```

From that home, `fdif` derives:

```text
current-transaction.json   local current-change selection
futurediff.sock            local daemon socket
runtime/                    daemon runtime and safe Git workspaces
futurediffd.log             daemon log
```

Set one alternative root with either:

```bash
fdif --home /path/to/fdif-home config --explain
```

or:

```bash
FDIF_HOME=/path/to/fdif-home fdif config --explain
```

Configuration precedence is:

```text
--home / --root
FDIF_HOME
FUTUREDIFF_ROOT (legacy)
~/.futurediff
```

The socket may still be overridden explicitly with `--socket` or
`FUTUREDIFF_SOCKET`.

`--state` is an advanced compatibility option. It relocates only the
current-selection JSON file; it does not change daemon data or workspace
placement. `fdif config --explain` shows every effective path and its source so
this distinction is visible.

## Path safety and macOS aliases

FutureDiff canonicalizes trusted operating-system aliases before creating or
using its private home. A path beneath macOS `/tmp`, for example, is used under
its canonical `/private/tmp` location.

This does not turn on general symlink following:

- a configured home that is itself a symlink is rejected;
- arbitrary user-controlled symlinked parent directories are rejected;
- the current-selection file may not be a symlink;
- daemon-root permissions remain private;
- the effective canonical path is shown by `fdif config --explain`.

## Recommended workflow

From a Git repository:

```bash
fdif start
```

`fdif new` is an alias. Normal output emphasizes:

1. where the safe working copy is;
2. that the current branch was not modified;
3. the next review and finish commands.

Internal details such as the transaction ID and cooperative mode are available
with:

```bash
fdif --verbose start
```

Open the safe path in an editor or coding agent, or run:

```bash
fdif shell
```

Review the result:

```bash
fdif review
fdif review --full
```

Finish the local workflow:

```bash
fdif finish
```

`finish` reviews the safe working copy, freezes the exact patch, runs checks,
resolves the canonical transaction digest, asks for exact approval, and
publishes:

```text
refs/heads/futurediff/<transaction-id>
```

The repository's current branch and current worktree remain unchanged.

## Optional GitHub draft pull request

When GitHub credentials are configured, select GitHub on the first finish run:

```bash
fdif finish --github
```

Local branch publication remains the foundation. FutureDiff pushes that exact
safe branch and creates a draft pull request. The provider request is prepared
before verification, so repository, base, head, title, and body are part of the
reviewed and approved transaction material.

See [`FDIF_GITHUB_PUBLICATION.md`](FDIF_GITHUB_PUBLICATION.md).

## Everyday commands

```text
fdif                         show the first-run/continue screen
fdif menu                    open the interactive numbered menu
fdif start | fdif new        create a safe working copy
fdif status                  show the current change
fdif workspace               print the safe working-copy path
fdif shell                   open a shell in the safe working copy
fdif review --full           show the exact diff
fdif finish                  verify, approve, and publish locally
fdif finish --github         also create a GitHub draft pull request
fdif abort | fdif discard    discard an unfinished change
fdif config --explain        explain effective paths and sources
fdif doctor                  check requirements and effective home
fdif demo --yes              run the disposable automated demo
```

## Cooperative-mode boundary

- FutureDiff isolates changes in a separate Git worktree.
- The user is responsible for running the editor or agent inside that path.
- FutureDiff verifies what exists at review time.
- Cooperative isolation is not an operating-system sandbox.
- Enforced rootless OCI execution remains experimental.

## Safety properties

- daemon peer authentication remains enabled by default;
- approval and publication require exact confirmation;
- non-interactive risky actions require `--yes`;
- approval material is resolved again immediately before mutation;
- GitHub branch creation is create-only and bound to the approved commit;
- credential IDs may appear in metadata, but tokens do not;
- JSON mode never prompts and contains no ANSI decoration;
- low-level FutureDiff exit codes are preserved;
- `fdif` does not duplicate transaction business logic;
- publication creates `futurediff/<transaction-id>` and never switches or
  mutates the current branch.
