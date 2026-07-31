# FutureDiff

**Review AI-assisted code changes before they reach GitHub.**

AI coding agents can modify repositories quickly, but users may not fully understand the exact change before an agent commits, pushes, or triggers an external action.

FutureDiff adds a local approval checkpoint. A human or coding agent works in an isolated Git workspace, FutureDiff shows the exact diff, runs configured checks, binds approval to the reviewed version, and publishes only that approved result as a safe branch. A draft GitHub pull request is optional.

> **Status:** `v0.1.0-alpha.1` candidate. Local-first, single-user, same-machine use on Linux and macOS. Windows runtime support is not yet claimed.

## See it work

```bash
make build
./bin/fdif doctor
./bin/fdif demo --yes
```

The disposable demo proves this flow:

```text
safe working copy
→ isolated edit
→ exact review
→ freeze reviewed version
→ verification
→ approval
→ safe local branch
```

The source branch remains unchanged.

## Quick start

### Build from source

Requirements:

- Go 1.23 or later;
- Git;
- a C toolchain;
- SQLite development headers.

```bash
git clone https://github.com/SHOnnay/futurediff.git
cd futurediff
make check
make build
./bin/fdif doctor
```

### Start a safe change

Inside a Git repository:

```bash
fdif start
# `fdif new` is an alias
```

FutureDiff prints the safe working-copy path. Open that path in your editor, terminal, or coding agent. FutureDiff Alpha does not launch or supervise the agent process.

```bash
fdif workspace
fdif shell
```

### Review the exact change

```bash
fdif review
fdif review --full
```

### Publish locally

```bash
fdif finish
```

FutureDiff guides the transaction through review, freezing, verification, approval, and publication. The result is a branch such as:

```text
futurediff/tx_77ad62755ed5618a2cc4e9a7
```

Your current branch is not rewritten.

### Optionally create a draft GitHub PR

```bash
fdif finish --github
```

FutureDiff displays the repository, base branch, head branch, and draft-PR details before confirmation. The local safe branch remains the core result; GitHub is optional.

## What FutureDiff protects

FutureDiff provides:

- an isolated Git working copy for each change;
- exact file and line-level review;
- configured verification and secret checks;
- approval bound to the exact reviewed material;
- safe publication to `futurediff/<transaction-id>`;
- optional draft GitHub pull-request creation;
- provider receipts and local audit events;
- explicit confirmation before publication.

FutureDiff does not automatically merge into your current branch.

## Everyday commands

| Command | Purpose |
|---|---|
| `fdif` | Open the guided menu |
| `fdif start` / `fdif new` | Start an isolated change |
| `fdif status` | Show the current change |
| `fdif workspace` | Print the safe working-copy path |
| `fdif shell` | Open a shell in the safe working copy |
| `fdif review --full` | Show the complete patch |
| `fdif finish` | Verify, approve, and publish locally |
| `fdif finish --github` | Also push and create a draft PR |
| `fdif abort` / `fdif discard` | Abort the current change |
| `fdif doctor` | Check dependencies and daemon health |
| `fdif demo --yes` | Run the disposable demo |

## How it works

FutureDiff has three public components:

```text
fdif          guided human-facing workflow
futurediff    exact low-level CLI and automation client
futurediffd   local transaction and publication daemon
```

A simplified lifecycle is:

```text
created → editing → sealed → verified → approved → published
```

The approved commit is materialized under:

```text
refs/heads/futurediff/<transaction-id>
```

GitHub and Slack actions are explicit provider effects. They are not required for local publication.

## Security model

FutureDiff Alpha is local-first:

- local daemon and local IPC;
- operating-system peer identity;
- fail-closed authentication;
- same-machine access;
- credentials referenced through restricted configuration rather than printed in commands or logs.

Linux uses kernel peer credentials. macOS uses native peer identity on the local Unix-domain socket. Do not expose the daemon or socket directly over a network.

See [SECURITY.md](SECURITY.md).

## Installation

The first public alpha release packages only these executables:

```text
fdif
futurediff
futurediffd
```

Planned release targets:

- Linux AMD64;
- Linux ARM64;
- macOS ARM64;
- macOS Intel.

Release archives include checksums. The documented installation path verifies the archive before copying binaries.

Until the first release is published, use the source-build instructions above. See [docs/FDIF_INSTALLATION.md](docs/FDIF_INSTALLATION.md).

## Current limitations

The alpha is intentionally narrow:

- local-first and same-machine only;
- single-user oriented;
- Linux and macOS runtime targets;
- Windows runtime support is not yet guaranteed;
- coding agents are started externally by the user;
- GitHub is the primary external code-review integration;
- Slack and rootless enforced execution remain experimental;
- no hosted-service, multi-tenant, formal SLO, or production-complete claim;
- no independent security-audit claim yet.

See [docs/LIMITATIONS.md](docs/LIMITATIONS.md).

## Documentation

Start here:

- [Quick start](docs/QUICKSTART.md)
- [Guided CLI](docs/FDIF_GUIDED_CLI.md)
- [Command reference](docs/FDIF_COMMAND_REFERENCE.md)
- [GitHub publication](docs/FDIF_GITHUB_PUBLICATION.md)
- [Installation](docs/FDIF_INSTALLATION.md)
- [Limitations](docs/LIMITATIONS.md)
- [Roadmap](ROADMAP.md)

Deep architecture, protocol, recovery, evidence, and assurance documentation remains under `docs/` for maintainers and reviewers. Internal task history is not part of the public product version.

## Development

```bash
make check
make build
./scripts/validate-overlay.sh
```

Build the three-binary public package for the current native platform:

```bash
make public-package
make verify-public-package
```

## Project principles

1. **Isolation before action.** Agent changes should not mutate the active source branch.
2. **Review exact material.** Approval must refer to a concrete patch and resulting commit.
3. **Fail closed.** Missing identity, stale state, damaged effects, or ambiguous recovery stops publication.
4. **Local value without providers.** A safe local branch is a complete outcome.
5. **Evidence over claims.** Public guarantees should be backed by repeatable tests and real demonstrations.

## Contributing

Contributions are most useful when they improve the core local transaction path, human-readable review, provider recovery, cross-platform authentication, installation, or tests for security-critical behavior.

Before submitting a change:

```bash
make check
make build
```

## License

[MIT](LICENSE)
