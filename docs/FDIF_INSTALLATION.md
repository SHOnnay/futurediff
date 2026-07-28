# Installing `fdif`

`fdif` should be installed beside `futurediff` and `futurediffd` so it can discover both automatically.

## Build through the repository

After merging this implementation, `fdif` is part of `COMMANDS`:

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

User-local destination is used by default. A custom destination:

```powershell
.\scripts\Install-Fdif.ps1 -Destination "$HOME\bin"
```

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

## First run

```bash
fdif doctor
fdif demo
```
