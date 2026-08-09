# Disaster Recovery

FutureDiff recovery prioritizes evidence preservation and prevention of duplicate effects.

- RPO target: 300 seconds or less.
- RTO target: 900 seconds or less.
- Unknown outcomes after recovery: zero before dependent effects resume.
- Every backup contains a canonical file manifest.
- Restore rejects traversal paths, symlinks, hard links, devices, and corrupt archives.
- A restored directory is accepted only after complete manifest verification.
- **Authenticate already-restored provenance**: a byte-identical backup is reported `already_restored: true` only when an authoritative backup-catalog record or a completed restore-evidence manifest proves the prior restore; uncatalogued byte-identical files are refused (fail closed).

Run `scripts/recovery-drill.sh` in CI and on the operational schedule defined by the deployment owner.
After recovery, verify both evidence layers before resuming high-risk mutation:

```bash
futurediff-audit --root ~/.futurediff
futurediff-audit --root ~/.futurediff --operator-events
```

## Certified drill evidence

The corruption/lock/disk-pressure certification drill (`scripts/certify-corruption-lock-disk-pressure.sh`) produces real local process/filesystem evidence under `docs/certification/corruption-lock-disk-pressure-<timestamp>/` with:
- 77/77 checks passing, 9/9 scenario blocks
- Classifications: `real_local` (chmod/chflags/ulimit/storage-policy), `deterministic_injection` (ADR-099 fault tests)
- Scenarios A–J covering stale lock, corrupt lock, live lock, ledger corruption, stale-backup refusal, fresh restore, storage classification, ambiguous ownership, audit corruption, ENOSPC-before-mutation, durability failure