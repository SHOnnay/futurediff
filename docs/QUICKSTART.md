# FutureDiff Quick Start

FutureDiff keeps AI-assisted changes in a separate Git working copy until you
review and approve the exact result.

## 1. Install or build

Use the latest GitHub prerelease archive for your platform, or build from
source:

```bash
git clone https://github.com/SHOnnay/futurediff.git
cd futurediff
make check
make build-public
export PATH="$PWD/bin:$PATH"
```

## 2. See the starting point

Run the command without a subcommand:

```bash
fdif
```

This prints a short starting screen in both interactive and non-interactive
terminals. The numbered menu is available explicitly:

```bash
fdif menu
```

## 3. Choose one FutureDiff home when needed

By default, FutureDiff keeps its local state, daemon files, socket, and safe
working copies under `~/.futurediff`.

For an isolated test or disposable environment, set one home:

```bash
export FDIF_HOME=/tmp/futurediff-test
fdif config --explain
```

On macOS, normal system aliases such as `/tmp` are resolved to their canonical
location before use. FutureDiff still rejects arbitrary user-controlled
symlink traversal and a home path that is itself a symlink.

The advanced `--state` option changes only the current-selection file. Use
`--home` or `FDIF_HOME` when the daemon and safe workspaces must move together.

## 4. Check the environment

```bash
fdif doctor
fdif demo --yes
```

The demo uses a disposable Git repository and does not require GitHub
credentials.

## 5. Start a real change

```bash
cd /path/to/your/repository
fdif start
```

FutureDiff prints the safe working-copy path and the next commands. Open that
path in your editor or coding agent. Your current branch is not modified.

## 6. Review the exact change

```bash
fdif status
fdif review --full
```

## 7. Publish safely

```bash
fdif finish
```

FutureDiff creates `futurediff/<transaction-id>` and leaves the current source
branch unchanged.

For optional GitHub review, configure a restricted credential and run
`fdif finish --github` on the first finish attempt. The result is a draft pull
request, not a merge.

## Next reading

- [Guided workflow and first-run behavior](FDIF_GUIDED_CLI.md)
- [Command reference](FDIF_COMMAND_REFERENCE.md)
- [GitHub publication](FDIF_GITHUB_PUBLICATION.md)
- [Installation](FDIF_INSTALLATION.md)
- [Limitations](LIMITATIONS.md)
