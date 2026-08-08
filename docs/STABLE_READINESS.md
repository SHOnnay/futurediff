# Stable-readiness status

FutureDiff `v0.1.0-alpha.3` is credible as a local-first alpha, but it is not yet ready for a stable release. The supported product remains:

- one local user on one machine;
- local Git repositories;
- Linux AMD64, Linux ARM64, macOS AMD64, and macOS ARM64 packaged runtimes;
- manual edit, manual approval, and manual publish;
- local-only daemon socket;
- optional GitHub draft-PR publication;
- no Windows runtime or installer support claim.

## Completed

### Git repository mutation boundary hardening

Completed in the merged `hardening/stable-readiness-git-boundary` milestone:

- guided Git helper subprocesses ignore ambient `GIT_DIR` and `GIT_WORK_TREE`;
- guided helper subprocesses ignore hostile global/system Git config, pager, hooks, external diff, fsmonitor, and credential prompting;
- guided entry rejects bare repositories and detached `HEAD` states before transaction creation;
- repository paths with spaces and shell metacharacters remain supported because Git execution stays argument-safe and shell-free.

### Protected-branch safeguards

These protections are enforced today:

- publication creates `refs/heads/futurediff/<transaction-id>` instead of mutating the current branch;
- guided flow re-checks exact approval material immediately before mutation;
- direct protected-branch mutation is not part of the supported guided workflow;
- commit, push, draft-PR, and merge remain explicit operator decisions.

### Explicit approval requirements

These are deliberate safeguards, not missing features:

- no implicit agent launch;
- no automatic merge;
- no direct protected-branch mutation;
- no approval bypass through the guided wrapper.

### Recovery behavior covered so far

Current completed coverage includes:

- stale source ref invalidation before release;
- durable lower-layer reconciliation for ambiguous external effects;
- durable abort/recover flows in the transaction kernel;
- guided rejection of unsupported repository shapes before work begins.

### Tests and platform coverage achieved

Current automated evidence includes:

- local unit and integration tests for guided Git-boundary hardening;
- hosted Linux, macOS, and Windows CI for the repository as a whole;
- native packaged-release validation only for Linux AMD64, Linux ARM64, macOS AMD64, and macOS ARM64;
- authenticated macOS peer-validation coverage.

Windows remains unsupported because no native Windows runtime, peer-auth, installer, or hosted release validation has been completed.

### Tamper-evident operator audit trail

Completed in the merged `hardening/immutable-audit-trail` milestone:

- append-only JSONL storage with hash chaining;
- deterministic redaction and no secret logging;
- crash-safe append and concurrent-writer protection;
- explicit verification tooling distinct from ordinary diagnostics;
- fail-closed recording before high-risk mutation;
- honest documentation of the local trust boundary.

The trail is tamper-evident, not tamper-proof. It does not claim protection against a fully privileged host administrator.

### Stable-default repository admission

Completed in this milestone:

- repository-admission policy version `0.2` is enforced automatically when the service has no custom policy;
- shallow repositories, replacement refs, grafts, alternate object databases, symlinked object directories, linked worktrees, detached HEAD, non-local HEAD refs, and unsupported ref formats fail closed unless a reviewed custom policy explicitly permits them;
- the Git subprocess boundary disables replacement-object interpretation even before admission evaluation;
- policy files must be bounded regular files and cannot be symlinks or group/world-writable on POSIX systems;
- decisions include stable reason codes and inspected repository facts for operator guidance and tests.

### Real GitHub write-and-recovery certification

Completed on 2026-08-02 against the disposable repository `SHOnnay/futurediff-certification-20260802143944-25328`:

- success path through the supported guided workflow (`fdif start` → workspace edit → `fdif finish --github`: seal, prepare GitHub effects, verify, approve, local `futurediff/*` branch, push, draft PR) with remote state independently verified via the GitHub API;
- denial paths certified before mutation: publish without approval, dirty worktree, detached HEAD, shallow-repository admission, direct default-branch mutation, unknown credential, empty-patch commit, and duplicate operations (idempotency);
- recovery drill: a controlled incomplete transaction (`needs_reconciliation` with prepared effects) was detected, recovered to a safe state, and resumed exactly once with no duplicate commit, push, or PR;
- default branch never mutated, no force push, no automatic merge;
- tamper-evident operator audit trail verified after certification (`valid: true`, 126 records);
- disposable repository archived after evidence verification; no unrelated repository touched; no secrets captured.

Evidence and the full report live in `docs/certification/GITHUB_WRITE_RECOVERY_2026-08-02.md` with machine-readable artifacts under `docs/certification/github-write-recovery-20260802/`. The exact sequence is recorded in `scripts/certify-github-write-recovery.sh`.
### Guided-recovery and stale-selection certification

Completed on 2026-08-02 with a fully local drill (`scripts/certify-guided-recovery.sh`, no network, no tokens):

- stale selection → `fdif recover --json` reports `selection_transaction_missing` with `selection_repaired: false`; `fdif recover --yes` clears the stale pointer (`selection_repaired: true`);
- deleted active safe working copy → `workspace_missing` with `workspace_available: false` and an explicit `fdif abort <id> --yes` recommendation; the guided CLI never recreates the workspace;
- interrupted sealed flow → `no_recovery_needed`, then the flow completes and commits exactly once;
- interrupted publication → the recovery planner refuses blind retry on ambiguous provider state (`query_status`) and re-arms only when the provider proves no mutation (`rearm_effect`); the daemon refuses a second `recover` on an already committed change (409 `recovery_failed`), proving recovery never double-publishes;
- hardened `current-transaction.json` store: bounded size, strict unknown-field rejection, symlink/permission rejection, atomic replacement, and ENOENT normalization for a file removed between stat and open (verified under `go test -race` with concurrent access).

Evidence lives in `docs/certification/guided-recovery-20260802-144845/` (`SUMMARY.json` plus per-scenario artifacts).

### Corruption, stale-lock, and disk-pressure certification

Completed 2026-08-03 with a fully local drill (`scripts/certify-corruption-lock-disk-pressure.sh`, no network, no tokens):

- live daemon → `fdif cleanup-lock --yes --json` refuses (`action: refused`, reason `lock_owner_alive`) and the lock is preserved;
- proved-stale lock (dead PID, previous boot) → cleanup removes the lock file and socket and records `event_type: lock_cleanup` in the operator audit trail; a repeated invocation is a no-op;
- corrupt (unparseable) lock → `fdif doctor --json` reports `daemon_lock` as `fail`, and `fdif cleanup-lock --yes` removes it;
- corrupt ledger → `fdif doctor --json` reports `ledger_integrity` as `fail` (distinct from the `warn` for a missing ledger) and runs event-chain validation;
- ledger restore refuses a backup older than the live ledger (`futurediff-restore` exits non-zero citing "older than the live ledger"); `--allow-stale-backup` overrides and applies with a pre-restore backup recorded;
- ledger restore refuses to run over a corrupt live ledger (fail-closed; the corrupt original is preserved) and applies cleanly into a fresh root;
- storage classification surfaced by `fdif doctor`.

Evidence lives in `docs/certification/corruption-lock-20260803-132704/` (`SUMMARY.json` plus per-scenario artifacts). Restore gating is also covered by deterministic Go tests in `internal/ledgerrestore/restore_test.go`; lock identity and cleanup semantics by `internal/daemonlock/lock_unix_test.go` and `internal/guidedcli/cleanup_lock_test.go`.

**New certification evidence**: `docs/certification/corruption-lock-disk-pressure-20260806-164815/` (77/77 checks, 9/9 scenarios, 0 failures; 33 evidence rows: 29 `real_local`, 4 `deterministic_injection`).

## Required before beta

- guided recovery for stale selections, deleted workspaces, and interrupted top-level flows — **completed 2026-08-02** (see the guided-recovery certification above);
- **corruption, stale-lock, and disk-pressure drills with operator guidance — completed 2026-08-06** (full 9-scenario certification: live-lock refusal, stale/corrupt cleanup with audit evidence, corrupt-ledger diagnosis, stale-backup restore refusal and override, fail-closed restore over corrupt ledger, storage classification, ambiguous ownership, audit corruption, ENOSPC-before-mutation, durability failure — see `docs/certification/corruption-lock-disk-pressure-20260806-164815/`);
- concrete provider-integration evidence for every supported provider surface.
## Required before stable

- signed release artifacts;
- published SBOM assets;
- reproducibility evidence for packaged releases;
- clean-machine install, upgrade, and uninstall evidence on supported platforms;
- compatibility and deprecation policy;
- external security review or authoritative external validation.

## Permanent safeguards

These should remain product safeguards even in stable releases:

- no automatic merge;
- no direct protected-branch mutation;
- no implicit agent launch;
- no Windows support claim without real native validation.

## Prioritized roadmap

### Critical

1. guided recovery and stale-selection hardening — **completed 2026-08-02** (see the guided-recovery certification above).

### High

1. **corruption, stale-lock, and disk-pressure drills — completed 2026-08-06** (full 9-scenario certification; see `docs/certification/corruption-lock-disk-pressure-20260806-164815/` and `docs/adr/ADR-099-corruption-lock-disk-pressure-resilience.md`).
2. stable release evidence set: signatures, SBOMs, reproducibility, install/upgrade/uninstall evidence;
3. real hosted GitHub write/recovery certification — **completed 2026-08-02** (see `docs/certification/GITHUB_WRITE_RECOVERY_2026-08-02.md`).
### Medium

1. supported-platform compatibility and uninstall contract;
2. decide whether enforced OCI graduates from experimental status.

### Deferred

1. Windows runtime and installer support.
