# Task 018 — One-command deterministic demonstration

## Goal

Provide a launch-quality demonstration requiring no model, API key, provider account, or container runtime.

## Implemented

- New `futurediff-demo` command.
- `scripts/demo.sh` wrapper.
- Temporary Git repository initialization.
- Transaction creation, detached staging, patch sealing, deterministic verification, digest-bound approval, and exact FutureDiff-ref publication.
- Machine-readable demonstration report.
- Failure exit when the live checkout changes unexpectedly.
- CI execution of the demonstration.

## Proven result

The executed demonstration produced:

- final transaction status: `committed`;
- live checkout value: `current reality`;
- FutureDiff ref value: `approved future`;
- live checkout safety: `true`.

The FutureDiff ref and live checkout were read independently from Git and the filesystem.
