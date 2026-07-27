# Task 051 — Maintenance-mode coordination

## Objective

Provide a daemon-wide, fail-closed mutation freeze for backup, restore, upgrade, and incident-response work.

## Implemented

- `futurediff-maintenance` command with `status`, `enable`, and `disable` actions.
- Digest-protected `maintenance.json` state under the FutureDiff data root.
- Optional automatic expiry with a maximum seven-day TTL.
- Daemon health output includes the current maintenance state.
- Every non-read HTTP operation returns `503 maintenance_mode` while enabled.
- Malformed or tampered maintenance state fails closed for mutations.
- State files are atomically published with mode `0600`.

## Example

```bash
futurediff-maintenance --root ~/.futurediff \
  --action enable --reason "offline ledger backup" --actor operator --ttl 30m

futurediff-maintenance --root ~/.futurediff --action status
futurediff-maintenance --root ~/.futurediff --action disable --actor operator
```

## Security boundary

Maintenance mode controls the daemon HTTP mutation surface. It is not a distributed lock and cannot prevent a separate privileged process from opening the ledger or repository directly. Offline tools must still verify that the daemon is stopped where required.

## Validation

- Mutation requests blocked with HTTP 503.
- Health remains readable.
- Automatic expiry restores mutation access.
- State-digest tampering is rejected.
- Normal and race tests pass.
