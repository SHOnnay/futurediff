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

## Current milestone

### Immutable operator audit trail

This milestone adds a separate local operator audit trail for security-sensitive daemon/API actions.

Required properties:

- append-only JSONL storage with hash chaining;
- deterministic redaction and no secret logging;
- crash-safe append and concurrent-writer protection;
- explicit verification command distinct from ordinary diagnostics;
- honest local trust-boundary documentation.

This trail is intended to be tamper-evident, not tamper-proof. It does not claim protection against a fully privileged host administrator.

## Required before beta

- real disposable GitHub write-and-recovery certification evidence;
- guided recovery for stale selections, deleted workspaces, and interrupted top-level flows;
- broader stable-default repository admission for hostile repository content and unsupported repository shapes;
- corruption, stale-lock, and disk-pressure drills with operator guidance;
- concrete provider-integration evidence for every supported provider surface.

## Required before stable

- immutable operator audit trail with verification tooling and recovery guidance;
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

1. stable-default repository admission hardening;
2. guided recovery and stale-selection hardening.

### High

1. immutable operator audit trail;
2. corruption, stale-lock, and disk-pressure drills;
3. stable release evidence set: signatures, SBOMs, reproducibility, install/upgrade/uninstall evidence;
4. real hosted GitHub write/recovery certification.

### Medium

1. supported-platform compatibility and uninstall contract;
2. decide whether enforced OCI graduates from experimental status.

### Deferred

1. Windows runtime and installer support.
