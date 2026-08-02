# FutureDiff Alpha Limitations

FutureDiff is an early local-first alpha. This document defines what the public
product does not yet promise.

## Supported scope

- one local user on one machine;
- local Git repositories;
- Linux and macOS runtime targets;
- cooperative workspace operation by default;
- manual editing or an externally launched coding agent;
- newcomer-oriented `fdif` starting and continuation guidance;
- one configurable FutureDiff home for daemon data and safe workspaces;
- safe local branch publication;
- optional GitHub draft-PR publication.

## Not supported or not guaranteed

- exposing `futurediffd` or its socket over a network;
- hosted, team, or multi-tenant operation;
- secure Windows runtime operation;
- launching or supervising coding-agent processes;
- automatic merging into the source branch;
- provider-generic pull-request publication beyond GitHub;
- guaranteed Slack delivery;
- formal uptime, disaster-recovery, retention, quota, RBAC, or SLO commitments;
- external security certification or independent audit;
- production-complete status.

## Cooperative mode

Cooperative mode creates an isolated Git working copy, but the user or agent
must actually work inside that copy. Rootless OCI enforcement exists as
experimental engineering work and is not a public-alpha guarantee.

## Paths and isolation

`--home` or `FDIF_HOME` relocates the daemon root, socket, current-selection
file, runtime directory, and safe workspaces together. The compatibility
`--state` option relocates only the current-selection file.

FutureDiff canonicalizes a small set of trusted operating-system path aliases
so normal macOS locations such as `/tmp` work. Arbitrary user-created symlink
traversal and a home that is itself a symlink remain rejected. This is path
hardening, not a complete filesystem sandbox.
## Recovery and stale selections

`fdif recover` reports recovery state and, only with explicit `--yes`, runs the
daemon's canonical recovery. The guided CLI never silently picks a different
change, silently aborts, silently recreates a deleted workspace, or deletes
evidence. Deleted safe working copies are not recreated; the recommended
explicit action is `fdif abort <id> --yes`.

Recovery of a transaction whose provider effect is ambiguous
(`needs_reconciliation` with unknown provider outcome) requires manual
inspection; the guided CLI reports `recovery_ambiguous` and never retries
blindly. See
[`adr/ADR-098-guided-recovery-and-stale-selection.md`](adr/ADR-098-guided-recovery-and-stale-selection.md).

## GitHub

GitHub credentials are optional. `fdif finish` publishes locally without them.
`fdif finish --github` requires an explicitly configured credential and
repository allowlist.

## Security reporting

Do not include tokens, private keys, private source, or raw evidence containing
secrets in public issues. Follow [SECURITY.md](../SECURITY.md).
