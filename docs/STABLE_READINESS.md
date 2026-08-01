# Stable-readiness gap analysis

FutureDiff `v0.1.0-alpha.3` is credible as a local-first alpha, but it is not yet stable-release ready. The current supported product remains:

- one local user on one machine;
- local Git repositories;
- Linux AMD64/ARM64 and macOS AMD64/ARM64 packaged runtimes;
- manual edit / manual approval / manual publish;
- local-only daemon socket;
- optional GitHub draft-PR publication;
- no Windows runtime or installer support claim.

## Architecture readout

The current architecture already has strong foundations:

- exact Git-tree staging and source-pin checks in `internal/staging`;
- durable SQLite state and recovery flows in `internal/ledger` and `internal/app`;
- kernel-authenticated local peer checks in `internal/peerauth` and `internal/api`;
- least-privilege credential brokering and exact destination scoping in `internal/credentials` and the provider adapters;
- release checksums, attestations, and prerelease packaging in `.github/workflows/public-alpha-release.yml` and `scripts/build-public-release.sh`.

The main remaining work is hardening the edges around that core: guided-CLI subprocess boundaries, repository-admission policy depth, crash and corruption drills, release operations, and externally witnessed evidence.

## Gap analysis

### 1. Security hardening

- **Command and argument injection:** Go subprocess calls avoid shell interpolation, but some guided-CLI Git calls were still inheriting ambient Git environment and config before this milestone.
- **Path traversal and symlink attacks:** the alpha already rejects unsafe home/state symlinks and checks release archives, but repository-content policy coverage is still narrower than a stable product should require.
- **Unsafe temporary files:** state writes are atomic and private; more corruption and disk-pressure drills are still needed.
- **Secret and credential leakage:** brokered credentials are scoped and redacted, but stable needs stronger end-to-end proof that local tooling, logs, and support bundles never re-expose them.
- **Malicious repository content / prompt injection:** cooperative mode intentionally is not a sandbox. Stable should add stronger repository-admission defaults and clearer redaction / allowlist rules for agent-visible artifacts.
- **Subprocess isolation:** staging and provider Git paths are hardened; guided-CLI helper Git paths needed the same treatment and are addressed by this branch.
- **Least-privilege filesystem access:** the local daemon root is private, but stable still needs more explicit policy and tests for what may be read, copied, exported, or attached.
- **Authentication and local peer verification:** Linux/macOS peer auth is in place; Windows remains unsupported.
- **Audit logging:** the ledger is strong, but the user-facing guided wrapper still needs a clearer immutable action trail for operator commands and cancellations.
- **Dependency and supply-chain risk:** attestations and checksums exist; stable should additionally publish signed artifacts and SBOM assets by default.
- **Rollback and recovery behavior:** lower layers have recovery concepts, but stable needs rehearsed guided-CLI recovery and corruption procedures.

### 2. Reliability

- interrupted transactions, unknown provider outcomes, and stale source refs already fail closed;
- corrupted guided state, partial workspace cleanup, stale user selections, and disk-exhaustion scenarios need deeper automated coverage;
- concurrent execution and stale lock handling exist at daemon level, but stable needs stronger operator guidance and tests around multi-terminal guided usage;
- retries and idempotency are good for external effects, but stable needs more explicit retry surfaces in the guided UX;
- provider timeouts and network failures are modeled, but still need real disposable-repo certification evidence.

### 3. Safe automation boundaries

- explicit approval before mutation, commit, push, PR, and merge remains a deliberate safeguard and should stay;
- protected-branch mutation is already avoided by design because publication creates `futurediff/<transaction-id>` instead of mutating the source branch;
- dry-run / plan-only behavior exists in lower layers and release tooling, but guided preview and audit surfaces still need expansion;
- immutable audit trail and emergency-stop ergonomics are incomplete from the top-level user workflow.

### 4. Platform readiness

- Linux AMD64, Linux ARM64, macOS ARM64, and macOS Intel are the only release targets with real packaging and hosted validation;
- Windows still needs native daemon peer-auth design, secure path handling, runtime validation, packaging, installer validation, and hosted native certification before any support claim.

### 5. Stable release operations

- prerelease packaging, checksums, GitHub attestations, and release automation are working;
- stable still needs default signed artifacts, published SBOMs, reproducibility checks, clean-machine install/upgrade/uninstall evidence, rollback evidence, and a documented compatibility/deprecation policy.

### 6. Product limitations

Some alpha limitations should remain deliberate product safeguards:

- no automatic merge;
- no direct protected-branch mutation;
- no implicit agent launch;
- no network-exposed daemon;
- no unsupported Windows support claim.

Those are safety boundaries, not merely missing features.

## Prioritized implementation plan

| Priority | Item | Current behavior | Risk | Proposed design | Affected components | Required tests | Completion criteria | Blocks beta | Blocks stable |
|---|---|---|---|---|---|---|---|---|---|
| Critical | Harden guided Git subprocess boundary | `fdif` helper Git calls existed outside the hardened staging/provider path | Global/system/repo Git config could trigger fsmonitor, pager, helper, or env-driven surprises | Run guided Git commands with minimized environment, disabled hooks/fsmonitor/pager/external diff, no credential prompting, no inherited `GIT_DIR` / `GIT_WORK_TREE` | `internal/guidedcli` docs/threat model | malicious global config, ambient `GIT_DIR`, review/start/demo regressions | guided helper Git paths are deterministic and ignore hostile ambient Git config | yes | yes |
| Critical | Expand repository-admission policy from “strict alpha” to “stable default” | strict checks reject tracked symlinks, submodules, and filters, but do not yet fully classify other hostile repository shapes | malicious repository content can confuse operators or agents | add stronger default repository policy plus explicit deny reasons surfaced through `doctor` / `start` | `internal/staging`, `internal/repoadmission`, guided CLI, docs | nested metadata, unsafe attributes, huge/binary policy, negative admission cases | stable default blocks unsupported repository structures before transaction creation | yes | yes |
| Critical | Guided recovery and stale-selection hardening | state file is atomic, but stale selections and partial cleanup need better top-level recovery UX | confusion after crashes or deleted workspaces | add guided recovery / reconcile command and clearer stale-state detection | `internal/guidedcli`, `internal/app`, docs | interrupted flow, missing workspace, stale source, concurrent session regression | user can recover or safely clear every interrupted guided state | yes | yes |
| High | Structured operator audit trail | durable ledger exists, but top-level operator actions are not summarized as a user-facing immutable trail | hard incident reconstruction and support | append hash-bound guided action receipts with tx/repo/outcome metadata | guided CLI, ledger/evidence docs | action logging, redaction, replay ordering | every guided mutation/cancel/publish action leaves a durable reviewable trace | no | yes |
| High | Corruption, stale-lock, and disk-pressure drills | daemon lock and ledger checks exist; guidance is thinner than stable needs | partial writes and support pain under host failure | add explicit drills and failure tests for full home, corrupt state, stale lock, missing runtime paths | guided CLI, doctor, maintenance scripts | disk-full, lock recovery, corrupt JSON/SQLite handling | deterministic operator guidance for local failure modes | yes | yes |
| High | Stable release evidence set | prerelease release flow produces checksums and GitHub attestations | insufficient stable supply-chain evidence | publish signed archives, SBOM assets, reproducibility evidence, clean-machine install evidence | workflows, release scripts, docs | SBOM generation, signature verify, reproducibility compare, install/upgrade/uninstall | stable release assets are signed, attestable, and reproducible | no | yes |
| High | Real hosted GitHub write/recovery certification | adapters have strong local tests | provider-edge regressions may still hide in real GitHub behavior | certify push/create/status/recovery on disposable repositories and bind evidence to release SHA | adapters, operations docs, hosted workflows | disposable repo write/recovery drills | exact release candidate has real provider evidence | yes | yes |
| Medium | Supported-platform upgrade/uninstall contract | install path is clear; upgrade/uninstall guarantees are informal | user breakage across prerelease iterations | define compatibility promises and uninstall cleanup expectations | installer scripts, docs | upgrade from prior prerelease, uninstall cleanup | documented and tested upgrade/uninstall path for supported platforms | no | yes |
| Medium | Enforced OCI graduation | OCI path is experimental | false expectation of sandboxing | either harden and certify rootless OCI, or keep it explicitly non-public | `internal/runtimeoci`, docs | real rootless docker/podman certification | evidence-backed decision on OCI support level | no | yes |
| Deferred | Windows runtime and installer support | build/test fragments exist but no secure supported runtime | false support claims and security holes | native peer-auth, path, packaging, installer, and hosted validation workstream | daemon, installer, workflows, docs | native Windows end-to-end validation | Windows stays unsupported until native secure validation passes | no | no |

## Milestone selected for this branch

**Critical:** harden the guided Git subprocess boundary.

This milestone was selected first because it closes a concrete security and reliability gap without weakening any manual approval control or changing supported-platform scope.
