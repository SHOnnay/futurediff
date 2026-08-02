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

## Required before beta

- real disposable GitHub write-and-recovery certification evidence;
- guided recovery for stale selections, deleted workspaces, and interrupted top-level flows;
- corruption, stale-lock, and disk-pressure drills with operator guidance;
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

1. guided recovery and stale-selection hardening.

### High

1. corruption, stale-lock, and disk-pressure drills;
2. stable release evidence set: signatures, SBOMs, reproducibility, install/upgrade/uninstall evidence;
3. real hosted GitHub write/recovery certification.

### Medium

1. supported-platform compatibility and uninstall contract;
2. decide whether enforced OCI graduates from experimental status.

### Deferred

1. Windows runtime and installer support.
