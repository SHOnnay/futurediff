# Provider-Integration Certification Report

- **Date**: 2026-08-09T05:50:01Z
- **Host**: Darwin-arm64
- **Git revision**: b0e77fb34e4b1596e5a347f7ccdb59ad54f82022
- **Nonce**: 20260809-114905
- **Confirmation phrase accepted**: yes

## Provider surfaces

Supported beta provider surfaces (certified):

| Surface | Adapter | Evidence classes | Status |
|---|---|---|---|
| GitHub branch publish | `builtin.github.branch-publish` | real_provider, deterministic_integration, historical_real_provider | Certified |
| GitHub draft pull request | `builtin.github.draft-pull-request` | real_provider, deterministic_integration, historical_real_provider | Certified |

Experimental surface (not part of the supported beta provider contract):

| Surface | Adapter | Evidence classes | Status |
|---|---|---|---|
| Slack message outbox | `builtin.slack.message-outbox` | deterministic_integration | Experimental — deterministic coverage recorded; real-mutation certification blocked on dedicated Slack token/channel; not certified for beta |

## Deterministic integration certification (always runs)

Focused Go tests for the provider adapters, the app-level external-effects
engine (commit, receipts, recovery, reconciliation), the credential broker
(scope, destination, source resolution), and the egress policy all pass.
Binary-level drills prove that provider preparation without a configured
broker, with an unknown credential, with a destination outside the credential
scope, and with an unset secret source is denied before any provider contact,
and that the daemon refuses an unsafe provider API base (fail-closed egress).

Artifacts: `deterministic/`.

## Historical real-provider certification (reused)

The 2026-08-02 real GitHub write-and-recovery certification
(`docs/certification/GITHUB_WRITE_RECOVERY_2026-08-02.md`,
`docs/certification/github-write-recovery-20260802/`) remains valid for the
provider-facing protocol surface: the runtime source of the three provider
adapters (`internal/adapters/githubbranch`, `internal/adapters/githubdraft`,
`internal/adapters/slackoutbox`, excluding test seams) is byte-identical to
the evidence commit `13b313b` (see `historical/provider-code-unchanged.txt`).
App-layer and credential-layer behavior since that commit is re-certified by
this run: the success path against the current code (real GitHub run below) and
the recovery/classification paths (deterministic suite above).

## Real provider certification (this run)

GitHub: a disposable private repository was created under the dedicated
certification account, the provider-cert mutation check (create commit, create
branch, create draft PR, close PR, delete branch) and the read-only readiness
suite ran against it, the GitHub CLI independently verified that no
certification branch or open certification PR remained and that the default
branch head was unchanged, and the repository was deleted. No canonical
repository and no real user data were touched.

Slack: see `blocked/` for the exact external prerequisite.

## Secret hygiene

`secrets-scan.txt` confirms that no credential material, authorization
header, or environment dump appears in the evidence artifacts.
