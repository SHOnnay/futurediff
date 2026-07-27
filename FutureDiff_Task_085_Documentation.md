# FutureDiff Task 085 — Verified backup catalog and bounded retention

## Objective

Reconcile ledger backup records with actual files and allow only bounded deletion of backups that still match their recorded evidence.

## Delivered

- `internal/backupcatalog` verifier, planner, and apply engine.
- `futurediff-backup-catalog` command.
- Versioned policy schema and example.
- Canonical backup-root containment.
- Regular-file and symlink checks.
- Exact file size and SHA-256 verification.
- SQLite integrity verification on a disposable copy.
- Keep-latest, minimum-age, and maximum-delete-byte constraints.
- Offline apply with exact confirmation: `DELETE_VERIFIED_FUTUREDIFF_BACKUPS`.
- Durable `backup_retention_actions` evidence.

## Safety properties

Missing, modified, non-regular, out-of-root, or SQLite-invalid backups make the catalog unhealthy and cannot be deleted. Files are reverified immediately before removal.

## Validation

A cataloged backup passed all checks and was safely removed with its ledger record. A second backup modified after creation was detected and caused a nonzero exit without deletion.
