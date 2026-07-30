# `fdif`: the guided FutureDiff CLI

`fdif` is the human-facing entry point for FutureDiff:

```text
futurediffd   authoritative local daemon
futurediff    exact, scriptable low-level client
fdif          guided command for people
```

The public-alpha workflow is local-first and cooperative. FutureDiff creates a
safe Git working copy. A human or an externally launched coding agent edits
that copy. FutureDiff then reviews, verifies, approves and publishes the exact
result as a new local branch.

FutureDiff does **not** launch or supervise coding agents in the alpha.

## Recommended workflow

From a Git repository:

```bash
fdif start
```

`fdif new` is a friendly alias for the same operation.

FutureDiff prints a safe working-copy path. Open that path in your editor or
coding agent. You can also open a terminal there:

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

`finish` is state-aware. It reviews the safe working copy, freezes the exact
patch, runs checks, resolves the canonical transaction digest, asks for exact
approval, and publishes:

```text
refs/heads/futurediff/<transaction-id>
```

The repository's current branch and current worktree remain unchanged.

## What success looks like

```text
Reviewed change published locally

Safe branch
  futurediff/tx_...

Current branch
  unchanged
```

The new branch can be inspected, merged, pushed, or used to open a pull request.
GitHub is an optional provider step, not a requirement for local success.

## Everyday commands

```text
fdif start | fdif new       create a safe working copy
fdif status                 show the current change
fdif workspace              print the safe working-copy path
fdif shell                  open a shell in the safe working copy
fdif review                 show changed files and a summary
fdif review --full          show the exact diff
fdif finish                 verify, approve and publish locally
fdif abort | fdif discard   discard an unfinished change
fdif doctor                 check local requirements
fdif demo --yes             run the disposable automated demo
```

The lower-level `seal`, `verify`, `approve`, and `publish` commands remain
available for advanced users and automation.

## Cooperative mode

Cooperative mode is the public-alpha default:

- FutureDiff isolates repository changes in a separate Git worktree.
- The user is responsible for running the editor or coding agent inside that
  safe working copy.
- FutureDiff verifies what is present at review time.
- Enforced rootless OCI execution remains experimental.

## Current change pointer

`fdif` stores only a local pointer at:

```text
~/.futurediff/current-transaction.json
```

It contains the transaction ID, repository root, and selection time. It
contains no approval digest or credential material. Writes are atomic,
permissions are restrictive, and symbolic-link paths are rejected.

## Safety

- daemon peer authentication remains enabled by default;
- approval and local publication require exact confirmation;
- non-interactive risky actions require `--yes`;
- approval material is resolved again immediately before mutation;
- JSON mode never prompts and never contains ANSI decoration;
- low-level FutureDiff exit codes are preserved;
- `fdif` does not duplicate transaction business logic;
- publishing creates a new `futurediff/<transaction-id>` branch and does not
  switch or mutate the current branch.
