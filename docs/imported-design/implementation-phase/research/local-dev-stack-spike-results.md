# Local-Dev Stack Spike Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This spike proves that one developer can run the current FutureDiff bootstrap stack locally with a boring setup:

- Go toolchain
- git
- PostgreSQL 18 binaries
- filesystem-backed artifact store
- host-shell worktree execution

## Implemented

- `futurediff/integrations/localdevstack/probe.go`
- `futurediff/integrations/localdevstack/probe_test.go`

## Proven behavior

- required local binaries are discovered:
  - `git`
  - `go`
  - `initdb`
  - `pg_ctl`
  - `psql`
  - `pg_dump`
- deterministic local workspace directories are created;
- artifact storage is writable;
- staged repo execution works in a worktree;
- disposable Postgres preview runs and verifies rollback;
- the bootstrap runtime mode is explicitly recorded as `host-shell-bootstrap`.

## Design correction

The research plan targeted Docker-compatible runtime as the first long-term isolation path.

The codebase now proves a slightly narrower truth first:

- the current bootstrap repo is runnable today with host-shell staging plus disposable Postgres;
- a Docker-backed staged-command path now exists, but it remains optional rather than the default local runtime mode.

That correction is healthier than pretending runtime isolation is further along than it is.

## Verification

- `go test ./...` passes, including `integrations/localdevstack`.

## Why this matters

This removes a common implementation trap: a good architecture with no credible contributor bootstrap path.

## Next useful move

Promote one supported runtime policy for local development and CI so host-shell and Docker-backed execution stop being merely optional seams and become explicit operating modes.
