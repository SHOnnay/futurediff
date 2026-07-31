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

## Optional GitHub draft pull request

When GitHub credentials are configured, select GitHub on the first finish run:

```bash
fdif finish --github
```

This keeps local branch publication as the foundation, then pushes that exact
safe branch and creates a draft pull request. The provider request is prepared
before verification, so the repository, base, head, title and body are part of
the exact reviewed and approved transaction material.

`--github` is optional. A missing GitHub configuration never prevents ordinary
local publication with `fdif finish`.

See [`FDIF_GITHUB_PUBLICATION.md`](FDIF_GITHUB_PUBLICATION.md) for credential
setup, options, confirmation behavior and recovery guidance.

## What success looks like

Local:

```text
Reviewed change published locally

Safe branch
  futurediff/tx_...

Current branch
  unchanged
```

GitHub:

```text
Reviewed change published and sent to GitHub

Safe branch
  futurediff/tx_...

Draft PR
  https://github.com/OWNER/REPOSITORY/pull/NUMBER

Current branch
  unchanged
```

## Everyday commands

```text
fdif start | fdif new       create a safe working copy
fdif status                 show the current change
fdif workspace              print the safe working-copy path
fdif shell                  open a shell in the safe working copy
fdif review                 show changed files and a summary
fdif review --full          show the exact diff
fdif finish                 verify, approve and publish locally
fdif finish --github        also create a GitHub draft pull request
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
- approval and publication require exact confirmation;
- non-interactive risky actions require `--yes`;
- approval material is resolved again immediately before mutation;
- GitHub branch creation is create-only and bound to the approved commit;
- draft-PR creation depends on the exact prepared branch effect;
- credential IDs may appear in transaction metadata, but tokens do not;
- JSON mode never prompts and never contains ANSI decoration;
- low-level FutureDiff exit codes are preserved;
- `fdif` does not duplicate transaction business logic;
- publishing creates `futurediff/<transaction-id>` and does not switch or mutate
  the current branch.
