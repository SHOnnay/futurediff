# FutureDiff Threat Model

## Protected assets

FutureDiff protects source repositories, transaction intent, approval material,
provider credentials, effect receipts, audit evidence, release artifacts,
policy configuration, local path configuration, and the local ledger.

## Trust boundaries

1. Agent or model input is untrusted.
2. The FutureDiff transaction kernel and ledger are trusted only after local
   integrity checks.
3. Cooperative Git workspaces isolate repository changes but are not operating-
   system sandboxes.
4. Enforced OCI workspaces are experimental isolated execution zones and must
   not receive repository metadata or provider credentials.
5. Provider adapters are separate effect boundaries.
6. CI runners and external providers are outside the local trust boundary.
7. Release artifacts are trusted only after checksum, provenance, content, and
   version verification.
8. The local operator audit trail is a tamper-evident record inside the local trust boundary. It detects ordinary modification, deletion, insertion, reordering, and truncation, but it does not claim protection against a fully privileged host administrator.

## Local path boundary

The guided CLI resolves one canonical FutureDiff home and passes it to the
local daemon. The current-selection file, default socket, runtime directory,
and safe-workspace root are derived from that home.

Normal operating-system aliases are not automatically attacker-controlled. In
particular, macOS exposes `/tmp` through `/private/tmp`. FutureDiff therefore
allows a narrowly enumerated set of platform aliases, resolves them before
use, and displays the canonical result.

It does **not** follow arbitrary symlinked path components. A configured home
that is itself a symlink, a user-created symlinked parent, and a symlink
current-selection file remain rejected. The daemon root must remain a private
real directory.

Residual risks include same-user time-of-check/time-of-use races and files an
editor or agent can access through the user's ordinary OS permissions.

## Primary threats and controls

| Threat | Required control |
|---|---|
| Agent bypasses approval | No direct commit or credential capability; effect release remains kernel-controlled |
| Stale or substituted approval | Approval binds to canonical transaction and artifact digests |
| Repository path escape | Reject arbitrary symlinks, special files, traversal paths, unsafe archives, and hidden Git metadata |
| Platform path alias rejected as hostile | Enumerate trusted OS aliases, canonicalize before use, and test native macOS behavior |
| Custom selection path misrepresented as workspace root | One home model, scoped `--state` compatibility option, and `config --explain` source reporting |
| Unsafe daemon root | Canonical real directory, private permissions, local socket, and daemon secure-root audit |
| Credential disclosure | Brokered credentials, minimized child environments, secret scanning, fingerprint-only findings |
| Git config, environment, or replacement-object injection | Guided Git subprocesses run with minimized environments, disabled hooks and fsmonitor, no external diff or pager, no inherited credential prompts, and replacement-object interpretation disabled |
| Detached, bare, shallow, history-substituted, or unsupported repository shape | Stable-default repository admission rejects detached HEAD, linked worktrees, shallow history, replacement refs, grafts, alternate object stores, symlinked object directories, non-local HEAD refs, and unsupported ref formats unless a reviewed custom policy explicitly allows a narrow exception |
| Duplicate external effect | Idempotency keys, durable attempts, receipts, and reconciliation |
| Unknown provider outcome | Persist unknown state; block dependent effects until reconciliation |
| Evidence tampering | SHA-256 manifests, deterministic bundles, provenance, and optional signatures |
| Operator action dispute or silent local policy denial | Separate operator audit trail with deterministic redaction, crash-safe append, and hash-chain verification |
| CI dependency compromise | Pin workflow actions to reviewed major or immutable commit references |
| Backup corruption | Embedded manifest, safe extraction, and full restore verification |
| Policy weakening or policy-file substitution | Versioned policies, stable fail-closed defaults, bounded regular policy files, symlink rejection, POSIX write-permission checks, reviewable diffs, readiness gate, and release-candidate evidence |

## Explicit non-claims

This repository does not prove provider behavior, rootless-runtime behavior,
native platform behavior, or hosted attestation unless corresponding real
external evidence is present and certified. Local simulation is not external
certification. Cooperative workspace isolation is not a complete filesystem or
process sandbox.

## Resilience controls (certified 2026-08-06)

The corruption/lock/disk-pressure drill validates the following threat mitigations:

| Threat | Control | Evidence |
|--------|---------|----------|
| SQLite header/page corruption | `fdif doctor` quick_check + `futurediffd --require-integrity` | `ledger_corrupt` reason code |
| WAL/SHM corruption | Startup integrity gate; quarantine preserves corrupt sidecars | `wal_inconsistent` reason code |
| Corrupt transaction/receipt/audit/backup metadata | Granular `IntegrityCheck` + bounded reads | `receipt_corrupt`, `audit_chain_invalid`, `backup_invalid` |
| Stale-lock without PID/StartTime/BootID proof | `lock_owner_ambiguous` fail-closed; cleanup requires `--yes` | `stale_lock_candidate` + `lock_owner_alive` |
| PID reuse / previous-boot lock | `isProcessAlive` validates StartTime and BootID on Linux/macOS | `lock_unix_test.go` `TestPIDReuse*`, `TestBootID*` |
| Disk pressure (ENOSPC/EDQUOT/EROFS/EIO) | `storageguard.Probe` classifies at every critical write | `disk_full`, `inode_exhausted`, `quota_exceeded`, `filesystem_read_only`, `durable_write_failed` |
| Audit-chain corruption | `Store.Verify()` detects hash/sequence gaps; `fdif doctor` surfaces scope | `audit_chain_invalid` |
| Duplicate external effects | Idempotency keys, durable attempts, receipts, reconciliation | `effect_reconciliation` in restore JSON |
| Unknown provider outcome | Persist unknown state; block dependent effects | `needs_reconciliation`, `recommended_action: fdif recover` |
| Evidence tampering | SHA-256 manifests, deterministic bundles, provenance | `MANIFEST.sha256`, certification drill `secrets-scan.txt` |
| Corrupt restore evidence destruction | Quarantine never auto-deleted; evidence manifest binds backup digest | `ledger-restore-evidence-*` directories |

The drill produces real local evidence (`real_local`) and deterministic fault-injection evidence (`deterministic_injection`) with zero secrets.