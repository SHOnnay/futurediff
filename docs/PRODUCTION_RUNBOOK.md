# Production Runbook

## Pre-deployment gate

Run `scripts/release-candidate.sh`. A deployable candidate requires a clean secret scan, approved license policy, passing tests, passing chaos checks, passing recovery drill, valid SBOM, valid provenance, and a verified deterministic source archive.

For a public production claim, replace the local policy with `config/production-readiness-policy.external.json` and provide a certified `external-evidence-report.json` covering every required target.

## Startup

1. Verify the release archive and checksum.
2. Verify the optional detached signature with the published public key.
3. Install into a new versioned directory.
4. Apply configuration through a reviewed, versioned policy bundle.
5. Start one daemon instance and verify its lock, health, ledger integrity, and provider readiness.
6. Enable traffic only after readiness passes.

## Incident handling

1. Stop new effect release while preserving evidence.
2. Record the incident start time and affected transaction identifiers.
3. Export a sanitized support bundle.
4. Reconcile unknown outcomes before retries.
5. Rotate potentially exposed credentials.
6. Restore from a verified backup only after preserving the failed ledger.
7. Document the root cause and corrective controls.

## Backup and recovery

Create backups from a quiesced or transactionally consistent data directory. Store the archive checksum separately. Test restoration regularly with `scripts/recovery-drill.sh`. Never extract untrusted archives using generic archive commands; use the safe restore command supplied by this overlay.

## Upgrade and rollback

Deploy side by side, run migrations with a verified backup available, and retain the prior binary and configuration. Roll back application binaries only when ledger/schema compatibility is confirmed. Never discard unknown-outcome records during rollback.
