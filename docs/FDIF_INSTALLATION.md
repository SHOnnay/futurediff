# Installing FutureDiff Alpha

`fdif`, `futurediff`, and `futurediffd` must be installed together so the guided CLI can discover the low-level client and daemon.

## Verified release archive

For a published release, download the archive and its checksum sidecar, then verify before extraction.

Example for macOS ARM64:

```bash
VERSION=v0.1.0-alpha.2
curl -fLO "https://github.com/SHOnnay/futurediff/releases/download/$VERSION/futurediff-$VERSION-darwin-arm64.tar.gz"
curl -fLO "https://github.com/SHOnnay/futurediff/releases/download/$VERSION/futurediff-$VERSION-darwin-arm64.tar.gz.sha256"
shasum -a 256 -c "futurediff-$VERSION-darwin-arm64.tar.gz.sha256"
```

Linux usually uses:

```bash
sha256sum -c "futurediff-$VERSION-linux-amd64.tar.gz.sha256"
```

Extract and inspect the archive before installation.

## Reviewable installer

Download the installer first, inspect it, then run it:

```bash
curl -fLo install-futurediff.sh \
  https://raw.githubusercontent.com/SHOnnay/futurediff/main/scripts/install-release.sh
less install-futurediff.sh
bash install-futurediff.sh --version v0.1.0-alpha.2 --prefix "$HOME/.local"
```

The installer refuses unsupported operating systems or architectures and verifies the release checksum before copying binaries.

To see the resolved asset without downloading:

```bash
bash install-futurediff.sh --version v0.1.0-alpha.2 --print-asset
```

## Build through the repository

Requirements:

- Go 1.23+;
- Git;
- C compiler;
- SQLite development headers.

```bash
make check
make build
ls -l bin/fdif bin/futurediff bin/futurediffd
```

Build and verify the native three-binary release archive from the repository:

```bash
make public-package
make verify-public-package
```

Checksum sidecars contain the archive basename, so verification is performed
inside the package directory by the Make target.

## Source installer on macOS and Linux

```bash
./scripts/install-fdif.sh --prefix "$HOME/.local"
```

This installer builds from the checked-out source. It is separate from the verified release installer.

Dry run:

```bash
./scripts/install-fdif.sh --dry-run --prefix "$HOME/.local"
```

## Windows

Windows binaries and completion generation remain build-tested, but the public alpha does not claim a complete secure Windows daemon and GitHub-provider runtime. The PowerShell source installer is retained for development only.

## Shell completion

Bash:

```bash
fdif completion bash > ~/.local/share/bash-completion/completions/fdif
```

Zsh:

```bash
mkdir -p ~/.zfunc
fdif completion zsh > ~/.zfunc/_fdif
```

Fish:

```bash
fdif completion fish > ~/.config/fish/completions/fdif.fish
```

PowerShell:

```powershell
fdif completion powershell | Out-File -Append -Encoding utf8 $PROFILE
```

## First run

```bash
fdif
fdif config --explain
fdif doctor
fdif demo --yes
```

Bare `fdif` prints a starting screen; the numbered menu is `fdif menu`.
FutureDiff uses `~/.futurediff` by default. Set `FDIF_HOME` to relocate local
state, daemon files, and safe workspaces together:

```bash
FDIF_HOME=/tmp/futurediff-test fdif config --explain
FDIF_HOME=/tmp/futurediff-test fdif demo --yes
```

The demo is local and disposable. It does not require GitHub credentials.

## Optional GitHub configuration

GitHub publication is configured through a restricted credential configuration file. Follow [FDIF_GITHUB_PUBLICATION.md](FDIF_GITHUB_PUBLICATION.md). Never paste a token into documentation, logs, command arguments, or chat.
