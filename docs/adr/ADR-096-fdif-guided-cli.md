# ADR-096: `fdif` is a guided layer over the canonical CLI

## Status

Proposed for merge.

## Decision

Ship a first-class Go binary named `fdif` as FutureDiff's normal human entry point. Keep `futurediffd` authoritative and keep `futurediff` as the exact, scriptable low-level client.

`fdif` uses the canonical client interface and parses its JSON responses. It does not implement an independent transaction state machine or release authority.

## Rationale

The raw transaction flow is intentionally explicit but requires users to manage long transaction IDs, workspace paths, verification files, and approval digests. That is appropriate for automation but unnecessarily difficult for interactive use.

A Go binary provides one behavior across macOS, Linux, Windows PowerShell, and editor terminals without adding Python or shell-runtime requirements.

## Safety consequences

- approval material is resolved automatically but never bypassed;
- exact confirmation remains mandatory for interactive approval and publication;
- current-transaction state contains no secrets;
- the source branch remains unchanged by publication;
- JSON and exit-code behavior remain scriptable;
- the low-level CLI remains backward compatible.

## Rejected alternatives

- Web UI: adds an unnecessary network and browser surface.
- Full-screen TUI: increases complexity without improving the core flow.
- Shell alias: inconsistent across shells and unusable as a Windows-native product layer.
- Replacing `futurediff`: would break scripts and blur the canonical authority boundary.
