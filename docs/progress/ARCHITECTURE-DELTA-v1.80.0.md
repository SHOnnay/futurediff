# FutureDiff Architecture v18.0 — Clean CLI terminal interface

## Presentation boundary

The terminal UI is a thin presentation layer around the canonical FutureDiff CLI. It does not own transaction state, authorization, effect execution, persistence, or provider reconciliation. Those responsibilities remain in the Go transaction platform.

## Automation boundary

Structured automation uses the canonical CLI or `futurediff-ui --json`. JSON and quiet modes contain no ANSI decoration. Exit codes from canonical commands are preserved.

## Safety boundary

Potentially destructive commands require explicit confirmation. Non-interactive execution requires `--yes`; likely credential values are redacted from wrapper command display. The wrapper never stores credentials.

## Capability boundary

Color, Unicode, and prompts are runtime capabilities rather than requirements. `NO_COLOR`, `--no-color`, non-TTY sessions, CI, and limited encodings receive portable plain output.

## Operational boundary

The status command reads signed or generated closure-status JSON but does not upgrade blocked evidence into a pass. The doctor command reports prerequisites but does not install or silently modify the host.
