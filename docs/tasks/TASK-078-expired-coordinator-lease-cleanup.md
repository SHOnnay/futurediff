# Task 078 — Expired coordinator lease cleanup

## Goal
Allow operators to inspect and remove only expired coordinator leases without risking deletion of active fencing state.

## Implemented

`futurediff-lease-cleanup` provides:

- offline-only operation protected by the daemon lock;
- dry-run lease inventory by default;
- acquired and expiry timestamps;
- explicit expired/live classification;
- exact confirmation phrase `DELETE_EXPIRED_FUTUREDIFF_LEASES`;
- deletion query constrained to `expires_at_ms <= now`.

The command never deletes a live lease even when apply mode is requested.

## Validation

A ledger containing one expired lease and one live lease produced one deletion. The live lease remained present.
