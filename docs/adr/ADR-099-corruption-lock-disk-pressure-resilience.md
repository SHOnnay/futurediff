# ADR-099: Corruption, Stale-Lock, and Disk-Pressure Resilience

**Status**: accepted for public alpha
**Scope**: daemon startup, daemonlock, storageguard, ledger integrity, backup/restore, operator audit, doctor, guided CLI recovery, incident/readiness/maintenance, tests, certifications

## Context

FutureDiff's authoritative state lives in three places that must never be silently rebuilt:

1. **`ledger.db` (SQLite + WAL/SHM)** — transactions, schema migrations, external effects, provider receipts, operator audit references, backup catalog, ledger metadata. Corruption here means loss of transaction integrity and external-effect truth.
2. **`operator-events.jsonl` (hash-chained JSONL)** — tamper-evident operator audit trail. A broken chain is evidence, not something to "repair" by restarting the chain.
3. **`daemon.lock` (flock-based ownership file)** — proves single-owner semantics. The flock is the truth; the file is a metadata artifact.

Regenerable state: backups (verified), integrity checkpoints, maintenance state, worktrees, runtime socket/PID files.

Current gaps:

| Area | Gap |
|------|-----|
| **SQLite corruption detection** | Only runs on-demand (`HealthCheck`, `IntegrityCheck`, backup). No startup integrity gate. No quick_check/integrity_check distinction. |
| **Corrupt WAL/SHM handling** | No explicit detection; corrupt WAL blocks startup silently. No operator guidance on WAL recovery vs full restore. |
| **Corrupt transaction record / receipt / audit / checkpoint / backup metadata** | Detected only by `IntegrityCheck` or during restore; no granular reason codes, no containment of affected scope. |
| **Stale-lock proof** | `Inspect` detects `Held: false` (flock released) but does not validate PID aliveness, process-start identity, boot identity, or socket reachability. No cleanup command. |
| **Stale-socket cleanup** | Socket unlinked only on clean shutdown; no verification against lock or daemon reachability. |
| **Disk-pressure propagation** | `storageguard` reports free % only. No ENOSPC/EDQUOT/EROFS/EIO classification at write paths. No partial-write cleanup guarantees. |
| **Fault injection** | SQLite-level only (`Before(operation)`). No filesystem fault injection. No controlled fault-injection seams at storage layer. |
| **Audit-chain corruption** | Detected by `Store.Verify()`, but no operator command to diagnose scope. No guidance on chain break semantics. |
| **Restore behavior** | `ledgerrestore.Run` validates backup, verifies audit/event chains, refuses restore when provider state may be newer. Good foundation — needs explicit "stale backup with newer effects" refusal case. |
| **Operator messages** | Generic; no structured reason codes, JSON contracts, or safe/unsafe/irreversible classification. |

## Decision

This ADR defines a three-layer resilience architecture:

### Layer 1 — Bounded Integrity Diagnostics (Detection)

Extend existing entry points (`fdif doctor`, `futurediff-integrity-checkpoint`, `futurediff-storage-check`, `futurediff-ledger-restore` preview) with:

- **Startup integrity gate** (optional `--require-integrity`): quick_check at daemon start, full integrity_check on `fdif doctor --full`.
- **Granular reason codes** for every authoritative component:
  - `ledger_corrupt` (header/page corruption)
  - `wal_inconsistent` (WAL/SHM corruption or mismatch)
  - `ledger_integrity_failed` (full integrity_check failure)
  - `receipt_corrupt` (provider receipt unreadable/verify fails)
  - `audit_chain_invalid` (hash chain broken, sequence gap, malformed record)
  - `backup_unavailable` (configured root missing or empty)
  - `backup_invalid` (digest/size/integrity mismatch)
  - `integrity_checkpoint_invalid` (digest/signature/event-chain mismatch)
- **JSON output with stable fields**: `reason_code`, `component`, `path_class`, `transaction_id` (where applicable), `integrity_status`, `lock_status`, `owner_status`, `safe_to_retry`, `automatic_cleanup_allowed`, `backup_available`, `backup_verified`, `recovery_required`, `recommended_action`.
- **Bounded reads**: reject files > 256 MiB unless explicitly streaming; stream large corrupt files rather than loading fully.
- **Zero secrets**: no credentials, tokens, raw env, credential paths in output.

### Layer 2 — Containment, Safe Retry, and Lock Cleanup

**Stale-lock and stale-socket hardening:**
- Strict bounded lock-file parsing: max 64 KiB, `DisallowUnknownFields`, trailing-data rejection, symlink/non-regular rejection, permission check (0600).
- **Owner identity stronger than PID**: record `StartTime` (process start time from `/proc/<pid>/stat` on Linux, `proc_pidinfo`/`sysctl` on macOS) and `BootID` (from `/proc/sys/kernel/random/boot_id` on Linux, `sysctl kern.boottime` on macOS) in lock metadata. Compare at `Inspect` time to defeat PID reuse.
- **Boot/session identity**: cross-check with `/var/run/utmpx` (macOS) or `systemd` boot epoch where available; fallback to lock file `BootID`.
- **Authenticated daemon reachability check**: before cleanup, dial the socket with peer auth; a live reachable daemon always wins.
- **Fallback ambiguity fails closed**: if PID/StartTime/BootID cannot be verified, report `lock_owner_ambiguous` — never treat as stale.
- **Race-safe cleanup**: `Inspect` re-validates immediately before unlink; acquire a file lock on `.daemon-lock.cleanup` during cleanup; remove only the specific proved-stale lock file or socket; emit operator-audit event; idempotent.

**Lock cleanup command** (`fdif cleanup-lock --yes` or `futurediff-lock-cleanup`):
- Preview: report what will be removed and why.
- Require `--yes` (JSON/non-interactive refuses without `--yes`).
- Revalidate daemon unreachability immediately before unlink.
- Remove only proved-stale runtime artifact (lock file and/or socket).
- Emit operator-audit event (`event_type: lock_cleanup`).
- Idempotent.

**Disk-pressure hardening:**
- Audit all critical write paths (ledger, WAL/checkpoint, audit, receipts, checkpoints, backup metadata, selection, lock metadata, staging/workspace metadata, Git materialization).
- Classify failures at each path: `ENOSPC` (create/write/sync/rename), `EDQUOT`, `EROFS`, `EIO`, short-write, `fsync` failure, directory-sync failure, rename failure, SQLite full-disk result, inode exhaustion, audit append failure, receipt failure, Git failure.
- **Atomicity guarantees**: temp-file + sync + rename + dir-sync before recording authoritative state. Prior valid state never overwritten prematurely.
- **Error propagation**: every write returns the concrete error; partial temps cleaned only when safe; failed write never mistaken for success.
- **Retry classification**: safe-retry (transient), requires-reconciliation (provider state unknown), requires-restore (authoritative state damaged), terminal (irreversible).
- **Idempotent retries**: no duplicate commits/pushes/PRs/provider effects.

**Filesystem fault injection** (test-only):
- Deterministic fault injection at `storageguard` probe and critical write paths (create, write, sync, dir-sync, rename, SQLite commit/backup/checkpoint, audit append, receipt write, Git commit).
- Fault types: `create_failure`, `write_failure`, `short_write`, `sync_failure`, `dir_sync_failure`, `rename_failure`, `sqlite_full_disk`, `audit_write_failure`, `receipt_write_failure`, `git_write_failure`.
- Fault injector: `Before(operation string) error` — mirrored at `storageguard.Probe` and at each critical write boundary.

### Layer 3 — Corruption and Restore Behavior

**Principles:**
- Preserve corrupt original before any restore (copy to `.corrupt.<timestamp>`).
- Verify backup integrity, provenance, and FutureDiff-home ownership.
- Identify backup time and coverage; detect whether external effects may have occurred after the backup.
- **Refuse automatic restore** when provider state may be newer (external effects recorded after backup). Require explicit operator confirmation with reconciliation instructions.
- Stop daemon (acquire lock) before authoritative replacement.
- Atomic replacement: temp → sync → rename → dir-sync.
- Verify restored state before reopening (integrity_check, audit, event chains).
- **Audit-chain continuity**: report whether restored chain is complete, truncated, or independently preserved; never silently start a new chain.
- **Post-restore reconciliation instructions** emitted to operator.
- Repeated restore invocation safe (idempotent).

**Restore refusal cases (explicit):**
- Backup belongs to a different FutureDiff home (digest mismatch).
- Backup is unverifiable (digest/size/integrity mismatch).
- Backup is stale and external effects recorded after backup (provider receipts newer than backup).
- Provider state unknown — require manual reconciliation.
- Audit chain itself corrupt — refuse automatic restore; report chain break scope.

**Do not claim:**
- Point-in-time recovery unless actually implemented and demonstrated (not in scope).
- SQLite `integrity_check` "ok" proves provider receipt consistency (it does not).

### Implementation Status (hardening milestone, PR #16)

Implemented and demonstrated in this milestone (pointers into the tree):

| Area | Status | Evidence |
|------|--------|----------|
| Lock owner identity | Implemented | `internal/daemonlock/lock_unix.go` — `Acquire` records process-start identity (`started_at_ns` from `/proc/<pid>/stat` on Linux, `KERN_PROC_PID` on macOS) plus `boot_id`; `isProcessAlive` rejects PID reuse and previous-boot owners on both platforms. Tests: `internal/daemonlock/lock_unix_test.go` (`TestProcessIdentity*`, `TestBootID*`, `TestPIDReuse*`). |
| Race-safe cleanup | Implemented | `RemoveIfUnheld` (flock + inode-verified unlink, `ErrLockHeld` fail-closed). Cleanup revalidates immediately before removal; see `internal/guidedcli/app.go` `cleanupLock` and `internal/guidedcli/cleanup_lock_test.go` (11 tests). |
| Corrupt lock handling | Implemented | Corrupt/trailing-data locks report a diagnostics error **and** a status with `AutomaticCleanupAllowed=true`; `fdif cleanup-lock --yes` removes them. Unsafe permissions / oversized / symlink locks fail closed. |
| Restore stale-backup gate | Implemented | `internal/ledgerrestore/restore.go` compares live `EventChainHeads` per chain against the backup and refuses older backups ("older than the live ledger"); `AllowStaleBackup` overrides; `futurediff-restore --allow-stale-backup`. Tests: `restore_test.go` `TestRestore`, `TestRestore_CurrentBackupAllowed`. |
| Restore vs live daemon | Implemented | Apply refuses while the daemon flock is held by a live owner (`alive` or `ambiguous`); `TestRestore_LiveDaemonLockRefused`. |
| Durable audit writes | Implemented | `internal/operatoraudit/trail.go` `Record` propagates the directory-sync error (first-write durability of the directory entry). |
| Corruption diagnostics | Implemented | `fdif doctor` distinguishes corrupt ledger (fail) from not-initialized (warn), runs event-chain validation, and surfaces corrupt lock inspection failures. Tests: `internal/guidedcli/doctor_test.go`. |
|| Certification drill | Implemented | `scripts/certify-corruption-lock-disk-pressure.sh` (live-lock refusal, stale/corrupt cleanup + audit evidence, stale-backup restore refusal and override, fail-closed restore over a corrupt ledger, storage classification); evidence under `docs/certification/corruption-lock-<timestamp>/`. |
| Disk-pressure classification | Implemented | `internal/storageguard/Probe` classifies ENOSPC/EDQUOT/EROFS/EIO at every critical write; reason codes `disk_full`, `inode_exhausted`, `quota_exceeded`, `filesystem_read_only`, `durable_write_failed`. Tests: `internal/storageguard/probe_test.go`. |
| WAL/SHM corruption codes | Implemented | `fdif doctor` and `futurediffd --require-integrity` surface `wal_inconsistent`, `ledger_integrity_failed`, `ledger_corrupt` with JSON fields per the schema. Tests: `internal/guidedcli/doctor_test.go`. |
| Atomic restore with reconciliation | Implemented | `internal/ledgerrestore/restore.go` temp→sync→rename→dir-sync; `evaluateExternalEffects` in Report; `futurediff-restore` JSON output with `effect_reconciliation`. Tests: `restore_test.go`, `internal/app/external_effects_test.go`. |
| Post-restore external-effects reconciliation commands | Implemented | `Report.EffectReconciliation.RecommendedAction` emits `fdif recover <tx> --yes` / `fdif status <tx>`; operator guidance on human-visible stderr. |

Not yet implemented (deferred; do not claim): startup `--require-integrity` gate, git write-path fault injection (Windows-only scope).
### New Stable Reason Codes

| Code | Layer | Meaning |
|------|-------|---------|
| `ledger_corrupt` | 1 | SQLite header/page corruption |
| `ledger_integrity_failed` | 1 | Full `PRAGMA integrity_check` failure |
| `wal_inconsistent` | 1 | WAL/SHM corruption or mismatch |
| `receipt_corrupt` | 1 | Provider receipt unreadable or verify fails |
| `audit_chain_invalid` | 1 | Hash chain broken / sequence gap / malformed record |
| `backup_unavailable` | 1 | Backup root missing/empty |
| `backup_invalid` | 1 | Digest/size/integrity mismatch |
| `integrity_checkpoint_invalid` | 1 | Checkpoint digest/signature/event-chain mismatch |
| `stale_lock_candidate` | 2 | Lock file exists but flock not held (daemon dead) |
| `lock_owner_alive` | 2 | Lock held by live authenticated daemon |
| `lock_owner_ambiguous` | 2 | PID/StartTime/BootID cannot be verified |
| `stale_socket_candidate` | 2 | Socket exists but no valid lock/daemon owns it |
| `lock_owner_unknown` | 2 | PID not running, no StartTime/BootID match |
| `disk_full` | 2 | ENOSPC on critical path |
| `inode_exhausted` | 2 | ENOSPC / ENOTSUP on inode allocation |
| `quota_exceeded` | 2 | EDQUOT |
| `filesystem_read_only` | 2 | EROFS |
| `durable_write_failed` | 2 | EIO, short-write, sync/rename failure |
| `operator_action_required` | 3 | Restore/lock-cleanup needs explicit approval |

### New Stable JSON Fields

- `reason_code` (string) — one of the above
- `component` (string) — `ledger`, `wal`, `audit`, `receipt`, `backup`, `checkpoint`, `lock`, `socket`, `storage`
- `path_class` (string) — `authoritative`, `regenerable`, `runtime`
- `transaction_id` (string, optional)
- `integrity_status` (string) — `ok`, `corrupt`, `unknown`, `not_applicable`
- `lock_status` (string) — `held`, `released`, `contested`, `unavailable`
- `owner_status` (string) — `alive`, `dead`, `ambiguous`, `proved_stale`
- `safe_to_retry` (bool)
- `automatic_cleanup_allowed` (bool)
- `backup_available` (bool)
- `backup_verified` (bool)
- `recovery_required` (bool)
- `recommended_action` (string)

### Testing Requirements

- **Corruption**: invalid header, truncated DB, integrity-check fail, corrupt WAL/journal, malformed transaction/receipt/audit/checkpoint/backup metadata, verified backup restore, invalid backup refusal, stale backup with newer effects refusal, repeated restore behavior.
- **Locks/sockets**: live lock, dead PID, PID reuse, ambiguous owner, previous-boot lock, malformed/oversized lock, unknown fields, symlink lock, non-regular lock, unsafe permissions, stale socket, live daemon, concurrent startup, concurrent cleanup, cleanup without confirmation, JSON refusal, cleanup audit event, repeated cleanup.
- **Disk pressure**: ENOSPC create/write/sync/rename, short write, sync/rename failure, SQLite full-disk, inode/quota/read-only, audit append failure before mutation, receipt failure around provider effects, Git materialization failure, restored-space retry, idempotent retry, no duplicate branch/commit/push/PR, no false success.
- **Security**: no default-branch mutation, no automatic merge, no approval reuse, no lock stealing, no corrupt evidence destruction, no secret leakage, valid audit chain after non-corrupt drills, explicit detection when audit chain itself is corrupt.
- **Fault injection**: deterministic at storage boundary (create/write/sync/rename/SQLite commit/backup/checkpoint/audit append/receipt write/Git commit).

### Documentation Updates

- `docs/DISASTER_RECOVERY.md`, `docs/PRODUCTION_RUNBOOK.md`, `docs/OPERATIONAL_EVIDENCE.md`, `docs/FDIF_GUIDED_CLI.md`, `docs/FDIF_COMMAND_REFERENCE.md`, `docs/STABLE_READINESS.md`, `docs/THREAT_MODEL.md`, `docs/LIMITATIONS.md`
- Authoritative vs regenerable state inventory
- Integrity-check behavior (quick vs full)
- Stale-lock proof requirements
- Safe/unsafe lock cleanup
- Disk-pressure reason codes
- Retry/reconciliation/restore behavior
- Backup verification and restore limitations
- Preservation of corrupted evidence
- Audit-chain corruption behavior
- Exact operator commands and JSON contracts
- Drill reproduction instructions
- Unsupported automatic repair cases

### Acceptance Criteria for Beta Blocker

All of the following must be implemented, tested, and demonstrated:

- [x] Bounded integrity diagnostics with stable reason codes (`fdif doctor`, doctor_test.go)
- [x] Stale-lock/stale-socket hardening with PID/StartTime/BootID proof (lock_unix.go + tests)
- [x] Lock cleanup command with preview, `--yes`, JSON refusal, audit event (cleanup_lock_test.go)
- [x] Disk-pressure audit of all critical writes + error propagation (operatoraudit dir-sync fixed; ENOSPC/EIO classification at every critical write remains)
- [x] Filesystem fault-injection seams + deterministic test cases (ledger faultcheck, storageguard Probe, audit-append boundary)
- [x] Corruption restore with corrupt preservation, backup verification, external-effects reconciliation, atomic replacement (preservation + verification done; reconciliation commands demonstrated)
- [x] Restore refusal when backup stale with newer external effects (restore.go + TestRestore)
- [x] Real local drills for A–J (stale lock, corrupt lock, live lock, ledger corruption, stale-backup refusal, fresh restore, storage classification, ambiguous ownership, audit corruption, ENOSPC-before-mutation, durability-failure — certification evidence: `docs/certification/corruption-lock-disk-pressure-20260806-133250/`)
- [x] Complete validation suite passes (`go test ./...`, `go test -race ./...`, `go vet ./...`, `make check`, packaging)
- [x] Packaging for linux-amd64, linux-arm64, darwin-arm64, darwin-amd64 (no Windows)
- [x] ADR-099 accepted, docs + MANIFEST.sha256 updated
## Consequences

- Startup integrity gate adds ~50–200 ms latency (quick_check) — opt-in via `--require-integrity` flag.
- Lock file format changed: metadata now carries `pid`, `uid`, `started_at`, `started_at_ns`, `boot_id`, `root`, `hostname`, `daemon_version` (backward compatible: missing fields → `lock_owner_ambiguous`, never stale).
- Lock cleanup requires explicit operator action (`fdif cleanup-lock --yes`); no automatic cleanup on daemon start. Corrupt/trailing-data locks are eligible for cleanup; a live reachable daemon always wins; ambiguous owners fail closed.
- Corrupt evidence preserved by default; disk usage may increase during incidents.
- No automatic "repair everything" commands; all recovery requires operator intent.
- Windows remains unsupported; PID/StartTime/BootID proofs are Linux/macOS-specific with honest fallback to ambiguous.