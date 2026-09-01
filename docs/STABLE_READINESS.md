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

### Provider-integration certification

Completed 2026-08-09 with `scripts/certify-provider-integrations.sh` (evidence in `docs/certification/provider-integrations-20260809-114905/`):

- every **supported** provider surface is certified: GitHub branch publish (`builtin.github.branch-publish`) and GitHub draft pull request (`builtin.github.draft-pull-request`); the Slack message outbox is **experimental, not a supported beta provider surface**, and is not claimed as certified for beta (see below);
- real GitHub mutation certification against a disposable private repository created and deleted by the run: create-only certification branch, draft pull request, close and delete cleanup, read-only readiness suite, and independent GitHub-CLI verification that no certification branch or open certification PR remained and the default-branch head was unchanged (`real_provider` evidence);
- deterministic integration evidence that always runs: focused provider/adapter/app/credential/egress tests plus binary-level scope-denial drills proving provider preparation without a configured broker, with an unknown credential, with a destination outside the credential scope, and with an unset secret source is denied before any provider contact, and that the daemon refuses a non-HTTPS provider API base (fail-closed egress) (`deterministic_integration` evidence);
- historical reuse: the 2026-08-02 real GitHub write/recovery certification remains valid because the provider adapter runtime source is byte-identical to the evidence commit `13b313b` (`historical_real_provider` evidence);
- **Slack message outbox is experimental**: the README supported-scope table lists Slack effects as Experimental and LIMITATIONS excludes guaranteed Slack delivery from the supported scope. Deterministic Slack coverage (post, status, metadata recovery, exactly-once reconciliation, token redaction) is recorded, but Slack real-mutation certification remains blocked on dedicated test credentials (`FUTUREDIFF_SLACK_TOKEN` plus a dedicated channel via `--slack`) and Slack is **not certified** for beta. Documented separately as remaining work before production in `WHAT_REMAINS_BEFORE_PRODUCTION.md`;
- evidence is credential-free: the run's secret scan reports zero leaks.
## Required before beta

- guided recovery for stale selections, deleted workspaces, and interrupted top-level flows — **completed 2026-08-02** (see the guided-recovery certification above);
- **corruption, stale-lock, and disk-pressure drills with operator guidance — completed 2026-08-06** (full 9-scenario certification: live-lock refusal, stale/corrupt cleanup with audit evidence, corrupt-ledger diagnosis, stale-backup restore refusal and override, fail-closed restore over corrupt ledger, storage classification, ambiguous ownership, audit corruption, ENOSPC-before-mutation, durability failure — see `docs/certification/corruption-lock-disk-pressure-20260806-164815/`);
- concrete provider-integration evidence for every supported provider surface — **completed 2026-08-09** for the declared beta scope: GitHub branch publish and GitHub draft pull request are fully certified (real + deterministic + historical evidence; see the provider-integration certification above). The Slack message outbox is **experimental and outside the supported beta contract**; its deterministic coverage is recorded, and its real-mutation certification remains blocked on dedicated Slack credentials.

### Release supply-chain integrity certification

Completed 2026-08-10 with `scripts/certify-release-supply-chain.sh` from the clean committed tree `251a173` (the release supply-chain implementation itself); current/final evidence in `docs/certification/release-supply-chain-20260810-140949-24564/` (`SUMMARY.json` `git_sha: 251a173…`, `failures: 0`, secret scan 0 findings). The earlier run bound to the pre-implementation base `82d5428` is preserved as historical evidence under `docs/certification/release-supply-chain-20260810-125405-32130/`:

- **signed release artifacts**: deterministic sign→verify roundtrip with an ephemeral in-script RSA-3072 keypair incl. tamper rejection, real packaged darwin-arm64 tarball + source zip signed and verified with the same keypair, independent recomputation (recorded public key + `.sig` re-verified); the private key never left the disposable run directory. Release-hosted signed assets and stable release-signing-key custody remain **blocked** (hosting requires a new release, which is forbidden for this milestone; custody is an operator decision);
- **published SBOM assets**: CycloneDX 1.5 SBOM create/verify/schema against the pristine HEAD worktree (1245 components) with a mutation negative test; real SBOM bound to HEAD. Publishing SBOM assets on a release remains **blocked** (release creation forbidden);
- **reproducibility evidence for packaged releases**: deterministic source zip built twice from the pristine worktree (identical sha256 + manifest digest, each `release-verify` clean); packaged build-twice from the same pristine worktree with a PATH-shimmed fixed `date` — payload file bytes are byte-identical (`payload_identical: true`), while the archives differ only in packaging metadata (tar entry mtimes and the gzip header written by BSD libarchive `tar -czf`), honestly classified `packaged_content_differs` with `byte_identical_packaged_archives` recorded **blocked** (SOURCE_DATE_EPOCH-style packaging normalization required; `gzip -n` re-packaging was never used as a pass basis);
- **clean-machine install, upgrade, and uninstall evidence**: deterministic drill (`deterministic_integration`, network dependency `github_release_download`) against the published `v0.1.0-alpha.1` → `v0.1.0-alpha.3` assets in a temp prefix + temp `FDIF_HOME`: install, upgrade, and uninstall exactly per the new compatibility/deprecation policy contract (remove `$prefix/bin/{fdif,futurediff,futurediffd}` + `$FDIF_HOME`); real drill re-installed the digest-verified published alpha.3 asset via the documented installer. Linux and Windows native clean-machine evidence remains **blocked** (no native hosts);
- **GitHub artifact attestations**: live read-only `gh attestation verify` **passed** against the digest-matched alpha.3 darwin-arm64 asset (`passed: true`, subject sha256 recorded) — real attestation evidence exists for the published alpha.3 asset;
- **compatibility and deprecation policy**: `docs/COMPATIBILITY_AND_DEPRECATION_POLICY.md` (new) defines versioning semantics, supported surfaces, the deprecation process, and the uninstall contract;
- **external security review** remains **blocked** (independent human/org; cannot be self-certified).

## Required before beta
## Required before stable

- signed release artifacts — **completed 2026-08-10** locally with an ephemeral keypair (deterministic roundtrip + real packaged sign/verify + independent recomputation; see the release supply-chain certification above); release-hosted signed assets and release-signing-key custody remain **blocked**;
- published SBOM assets — **completed 2026-08-10** locally (CycloneDX 1.5 create/verify/schema + mutation negative; real SBOM bound to HEAD); publishing SBOM assets on a release remains **blocked**;
- reproducibility evidence for packaged releases — **in progress** (darwin-arm64: payload_identical=true, archive_identical=true; linux targets and per-target reproducibility evidence pending GitHub-hosted runner execution);
- clean-machine install, upgrade, and uninstall evidence on supported platforms — **completed 2026-08-10** for macOS arm64 (published alpha.1 → alpha.3 drill + real drill; see the certification above); Linux and Windows native hosts remain **blocked**;
- compatibility and deprecation policy — **completed 2026-08-10** (`docs/COMPATIBILITY_AND_DEPRECATION_POLICY.md`, including the uninstall contract the drill executes);
- external security review or authoritative external validation — **blocked** (independent human/org; cannot be self-certified).

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
2. stable release evidence set: signatures, SBOMs, reproducibility, install/upgrade/uninstall evidence — **completed 2026-08-10** (see the release supply-chain integrity certification above; hosted/published/external items remain blocked with exact prerequisites);
3. real hosted GitHub write/recovery certification — **completed 2026-08-02** (see `docs/certification/GITHUB_WRITE_RECOVERY_2026-08-02.md`).
### Medium

1. supported-platform compatibility and uninstall contract — **completed 2026-08-10** (`docs/COMPATIBILITY_AND_DEPRECATION_POLICY.md`; the uninstall contract is exercised by the release supply-chain drill);
2. decide whether enforced OCI graduates from experimental status.

### Deferred

1. Windows runtime and installer support.
