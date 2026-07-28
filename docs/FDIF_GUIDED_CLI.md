# `fdif`: the guided FutureDiff CLI

`fdif` is the human-facing entry point for FutureDiff. It keeps the canonical architecture intact:

```text
futurediffd   authoritative local daemon
futurediff    exact, scriptable low-level client
fdif          guided terminal and PowerShell experience
```

It is deliberately not a web application, desktop interface, or full-screen TUI. Running `fdif` opens a small numbered menu. Direct subcommands remain available for experienced users and automation.

## Recommended workflow

```bash
cd /path/to/repository
fdif start
```

FutureDiff prints the isolated workspace. Make changes there, then run:

```bash
fdif finish
```

`finish` is state-aware. It reviews the workspace, seals the exact patch, runs verification, obtains the canonical transaction digest, asks for approval, and publishes the approved change branch.

The user never needs to copy a transaction ID, workspace path, approval digest, patch hash, tree object ID, or daemon socket path.

## What publishing means

FutureDiff does not silently modify the repository's current branch. The canonical commit operation publishes:

```text
refs/heads/futurediff/<transaction-id>
```

The user can inspect, merge, or open a pull request from that branch. `fdif` uses “publish” as its preferred user-facing term. `apply` and `commit` remain aliases for compatibility.

## Current transaction

`fdif` stores only a local pointer at:

```text
~/.futurediff/current-transaction.json
```

The file contains the transaction ID, repository root, and selection time. It contains no approval digest or credential material. Writes are atomic, the file uses restrictive permissions, and symbolic-link paths are rejected.

Resolution order:

1. explicit transaction ID;
2. saved current transaction;
3. the only eligible open transaction;
4. interactive numbered selection;
5. a clear error.

## Safety

- daemon peer authentication remains enabled by default;
- approval and publish require exact confirmation;
- non-interactive risky actions require `--yes`;
- approval material is re-resolved immediately before mutation;
- JSON mode never prompts and never contains ANSI decoration;
- low-level FutureDiff exit codes are preserved;
- `fdif` does not duplicate transaction business logic.
