# FutureDiff clean terminal UI

FutureDiff remains CLI/API-first. This interface improves terminal usability without adding a web dashboard, desktop application, or heavy terminal framework.

## Principles

- Machine behavior remains stable and scriptable.
- `--json` contains no ANSI sequences, headings, or progress decoration.
- `--quiet` suppresses wrapper decoration.
- `NO_COLOR` and `--no-color` disable ANSI color.
- CI and non-interactive sessions never block on ordinary prompts.
- Potentially destructive commands require `YES` interactively or `--yes` non-interactively.
- Likely credential arguments are redacted before command echoing.
- The canonical FutureDiff process retains its real stdout, stderr, signals, and exit code.
- Unicode symbols fall back to portable text when the terminal encoding is unsuitable.

## Commands

```bash
./scripts/futurediff-ui doctor
./scripts/futurediff-ui status --status-dir dist/closure
./scripts/futurediff-ui config
./scripts/futurediff-ui completion bash
./scripts/futurediff-ui exec -- transaction-list
```

Unrecognized commands are passed through directly:

```bash
./scripts/futurediff-ui transaction-list
./scripts/futurediff-ui --json status --status-dir dist/closure
./scripts/futurediff-ui --yes access-cleanup --apply
```

## Shell completion

```bash
./scripts/futurediff-ui completion bash > ~/.local/share/bash-completion/completions/futurediff-ui
./scripts/futurediff-ui completion zsh > ~/.zfunc/_futurediff-ui
./scripts/futurediff-ui completion fish > ~/.config/fish/completions/futurediff-ui.fish
```

## Exit behavior

- `0`: wrapper command succeeded or canonical command exited successfully.
- `1`: doctor found a required local prerequisite failure.
- `2`: wrapper usage, safety, or configuration error.
- `4`: status is valid but production completion remains blocked.
- Other codes: preserved from the canonical FutureDiff executable.
