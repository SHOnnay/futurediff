# Installing `fdif`

`fdif` should be installed beside `futurediff` and `futurediffd` so it can
discover both automatically.

## Build through the repository

```bash
make build
ls -l bin/fdif bin/futurediff bin/futurediffd
```

## macOS and Linux

```bash
./scripts/install-fdif.sh
```

The default prefix is `/usr/local`. A user-local installation:

```bash
./scripts/install-fdif.sh --prefix "$HOME/.local"
```

Ensure `$HOME/.local/bin` is on `PATH`.

Dry run:

```bash
./scripts/install-fdif.sh --dry-run --prefix "$HOME/.local"
```

## Windows PowerShell

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\Install-Fdif.ps1
```

Windows binaries and completion generation are build-tested. The public alpha
does not yet claim a complete secure Windows daemon and GitHub-provider runtime.

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

PowerShell profile:

```powershell
fdif completion powershell | Out-File -Append -Encoding utf8 $PROFILE
```

## First local run

```bash
fdif doctor
fdif demo --yes
```

The demo is local and disposable. It does not require GitHub credentials.

## Optional GitHub configuration

GitHub publication is configured at daemon startup through a restricted
credential configuration file. Start with:

```bash
mkdir -p "$HOME/.config/futurediff"
cp examples/credentials/providers.example.json \
  "$HOME/.config/futurediff/providers.json"
chmod 600 "$HOME/.config/futurediff/providers.json"
```

Edit the destination allowlist for the intended repository, export the token
source named by the config, then:

```bash
export FUTUREDIFF_CREDENTIAL_CONFIG="$HOME/.config/futurediff/providers.json"
export FUTUREDIFF_GITHUB_CREDENTIAL_ID=github-main
fdif daemon restart
fdif doctor
```

Full setup and safe usage are documented in
[`FDIF_GITHUB_PUBLICATION.md`](FDIF_GITHUB_PUBLICATION.md).
