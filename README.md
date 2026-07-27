# FutureDiff

FutureDiff is a transactional effect layer for autonomous AI agents.

> Existing agents decide what to do. FutureDiff controls whether and how those decisions become persistent reality.

## Current milestone: Task 045

The canonical implementation is Go and supports the staged transaction chain below, plus a unified real-environment certification command:

```text
exact local repository change
        ↓
create-only GitHub futurediff/* branch
        ↓
dependent GitHub draft pull request
        ↓
dependent Slack outbox message
```

A generic MCP stdio bridge lets MCP-capable agents stage and verify this future without exposing approval or commit authority.

Implemented capabilities:

- private Unix-socket daemon and trusted CLI;
- durable SQLite transaction ledger;
- detached Git staging and deterministic commit identities;
- rootless OCI source implementation;
- deterministic verification;
- credential broker with exact operation/destination scopes;
- durable effect dependency graph;
- create-only GitHub branch publication;
- dependent GitHub draft pull requests;
- Slack durable outbox with ambiguous-result recovery;
- coordinator leases and fencing tokens;
- write-ahead provider attempts and durable receipts;
- generic MCP stdio integration;
- content-addressed FuturePack evidence export.
- tamper-evident per-transaction event chains;
- read-only ledger invariant auditing;
- confirmation-gated terminal artifact retention;
- operator diagnostics and API-contract compatibility checks;
- verified transaction `.futurepack` forensic export;
- confirmation-gated offline ledger restoration;
- event-log projection replay;
- configuration linting;
- semantic API contract diffing;
- EffectSpec lifecycle conformance testing;
- verification-policy explanation and simulation;
- no-blind-retry recovery drills;
- privacy-preserving JSON/Prometheus metrics;
- verified redacted support bundles.

## Repository provenance

This repository contains the canonical runnable Go core plus useful specifications and research imported from the supplied `futurediff-design` branch. The original experimental branch is preserved under `research/original-design-branch/` and is not silently mixed into the trusted runtime. See `docs/MERGE-REPORT.md` and `PROVENANCE.md`.

## Requirements

Cooperative mode:

- Go 1.23+
- Git
- C compiler
- SQLite development library

Enforced mode additionally requires:

- rootless Docker or rootless Podman;
- a digest-pinned OCI image (`name@sha256:<digest>`).

## Build and validate

```bash
make check
make build
```

Equivalent commands:

```bash
gofmt -w .
go vet ./...
go test -race ./...
go build ./cmd/...
```

The build produces 30 commands, including the daemon, trusted CLI, MCP bridge, certification, audit, export, restore, replay, configuration-lint, and API-diff tools. Run `ls bin/` after `make build` for the complete inventory.

## Start the daemon

```bash
chmod 600 examples/credentials/providers.example.json
export FUTUREDIFF_GITHUB_TOKEN='test-or-production-token'
export FUTUREDIFF_SLACK_TOKEN='test-or-production-token'

./bin/futurediffd \
  --credential-config examples/credentials/providers.example.json
```

Provider secrets are not exposed through the public API and are not stored in SQLite.

## Repository + GitHub branch + PR + Slack transaction

```bash
# Create a cooperative transaction.
./bin/futurediff create /path/to/repository cooperative

# Edit the returned detached transaction workspace, then seal it.
./bin/futurediff seal <transaction-id>

# Prepare publication of the deterministic approved commit.
./bin/futurediff prepare-github-branch <transaction-id> \
  github-main acme app futurediff/<transaction-id> \
  https://github.com/acme/app.git

# The returned effect ID becomes the PR dependency.
./bin/futurediff prepare-github-pr <transaction-id> \
  github-main acme app futurediff/<transaction-id> main \
  'FutureDiff change' 'Prepared transaction' <branch-effect-id>

# Slack may depend on the PR effect.
./bin/futurediff prepare-slack-message <transaction-id> \
  slack-main C0123456789 'FutureDiff transaction released' <pr-effect-id>

./bin/futurediff verify <transaction-id> examples/verification/basic.verify.json
./bin/futurediff approval-material <transaction-id>
./bin/futurediff approve <transaction-id> <transaction-digest>
./bin/futurediff commit <transaction-id> <transaction-digest>
```

If a provider mutation has an ambiguous transport result, run:

```bash
./bin/futurediff recover <transaction-id>
```

Recovery queries provider status using exact effect identities rather than blindly issuing the mutation again.

## MCP bridge

Start the daemon first, then configure an MCP client to launch:

```bash
/path/to/futurediff-mcp --socket ~/.futurediff/futurediff.sock
```

A generic example is in `examples/mcp/generic-client.example.json`.

The MCP bridge exposes staging, inspection, deterministic verification, and effect preparation. It deliberately does **not** expose approval or commit tools. Release remains a trusted local action.


## Certification suite

Generate a machine-readable local certification report:

```bash
./bin/futurediff-cert-suite \
  --target local \
  --mcp-binary "$PWD/bin/futurediff-mcp" \
  --socket "$HOME/.futurediff/futurediff.sock" \
  --output certification.json
```

External targets include `oci`, `github`, `slack`, `opencode`, `hermes`, and
`attestation`. Missing prerequisites are reported as `blocked`, never as passed.
See `examples/certification/README.md`.

## Operating modes

### Cooperative

Provides durable staging and provider-effect orchestration, but does not claim every host-side bypass path is prevented.

### Enforced

Requires a rootless OCI runtime and digest-pinned image. Agent-authored commands run without provider credentials, with network disabled by default, and without mounting the live checkout or Git metadata.

Real rootless-host certification remains pending.


## Installation

Review and apply a local installation plan:

```bash
futurediff-install --source-dir /path/to/release \
  --prefix "$HOME/.local" \
  --root "$HOME/.futurediff" \
  --service systemd-user
```

Use `--dry-run` to print the exact file plan without writing. Linux systemd-user and macOS launchd-user service definitions are supported. The installer never enables provider credentials automatically.

## Platform support

```bash
futurediff-platform --output platform.json
```

Linux amd64 is supported. Linux arm64 and macOS are experimental native targets. Windows is explicitly unsupported until named-pipe transport and Windows credential isolation are implemented.

## Measured agent benchmark

```bash
futurediff-agent-bench \
  --input direct-run.json \
  --input futurediff-run.json \
  --baseline direct \
  --json report.json \
  --markdown report.md
```

The command aggregates only supplied measurements. It does not infer token or latency values.

## Offline release verification

```bash
futurediff-verify-release \
  --source futurediff-v0.29.0-linux-amd64.tar.gz \
  --output verification.json
```

This checks archive safety, SHA-256 entries, SPDX structure, and in-toto/SLSA subject digests. Add `--require-signed-attestation` for GitHub attestation verification.

## Disposable provider mutation certification

This command performs real, visible test mutations and requires a dedicated repository/channel plus the exact confirmation phrase:

```bash
futurediff-provider-cert \
  --target github --target slack \
  --confirm-provider-mutations I_UNDERSTAND_FUTUREDIFF_WILL_CREATE_AND_CLEAN_UP_TEST_RESOURCES \
  --github-owner OWNER --github-repo DISPOSABLE_REPO \
  --github-token-env FUTUREDIFF_GITHUB_TEST_TOKEN \
  --slack-channel TEST_CHANNEL_ID \
  --slack-token-env FUTUREDIFF_SLACK_TEST_TOKEN
```

GitHub resources are closed/deleted and the Slack message is deleted. Use only disposable test resources.

## Documentation

- `ARCHITECTURE.md`
- `docs/tasks/TASK-011-controlled-github-branch-publication.md`
- `docs/tasks/TASK-012-slack-durable-outbox.md`
- `docs/tasks/TASK-013-generic-mcp-bridge.md`
- `docs/progress/PROGRESS-AUDIT-TASK-013.md`
- `docs/daemon/api-v0.5.md`
- `docs/tasks/TASK-041-effectspec-conformance-kit.md`
- `docs/tasks/TASK-042-policy-explanation-and-simulation.md`
- `docs/tasks/TASK-043-recovery-planner-drill.md`
- `docs/tasks/TASK-044-privacy-preserving-metrics.md`
- `docs/tasks/TASK-045-redacted-support-bundle.md`

## Current progress estimate

- architecture and research: **99%**;
- public open-source MVP: **98.5%**;
- production-grade platform: **70%**.

These are weighted acceptance-criteria estimates, not line-count estimates.

## One-command demonstration

```bash
./scripts/demo.sh
```

This requires Git, Go, and SQLite development libraries, but no model, API key, container runtime, or provider account. It proves that the approved FutureDiff ref changes while the live checkout remains unchanged.

## Host-specific rootless certification

```bash
futurediff-certify \
  --runtime docker \
  --image alpine@sha256:<64-hex-digest> \
  --output certification.json
```

Certification is specific to the host, runtime, and image digest. Source-level support alone is not treated as enforced-mode certification.

## Deterministic effect-safety benchmark

```bash
make benchmark
```

The initial benchmark models release, duplication, approval, and recovery semantics. It is deliberately labelled synthetic and does not claim to measure model quality or real provider latency.

## Ledger maintenance

```bash
futurediff-admin --root ~/.futurediff
futurediff-admin --root ~/.futurediff --backup ~/backups/futurediff-ledger.db
```

## Release artifacts

Tagged releases contain all command binaries, embedded build metadata, SPDX 2.3 SBOM, SHA-256 checksums, architecture documentation, and provenance notes.

## Agent integration profiles

Generate an OpenCode profile:

```bash
futurediff-integrate --target opencode \
  --mcp-binary /absolute/path/futurediff-mcp \
  --socket "$HOME/.futurediff/futurediff.sock" \
  --output ./opencode.futurediff.json
```

Generate a Hermes Agent MCP entry:

```bash
futurediff-integrate --target hermes \
  --mcp-binary /absolute/path/futurediff-mcp \
  --socket "$HOME/.futurediff/futurediff.sock" \
  --output ./hermes-futurediff.yaml
```

The generated profiles expose staging and verification tools only. Approval, commit, and credentials remain outside the agent tool surface.

## Ledger fault self-test

```bash
futurediff-admin --root /tmp/fd-admin \
  --fault-self-test /tmp/fd-faults
```

Use only a disposable fault-test directory.

## Release provenance

`futurediff-provenance` emits an in-toto/SLSA provenance statement for local artifacts. Tagged GitHub releases also use GitHub artifact attestations; local provenance files are not signatures.


## Operations and integrity

```bash
# Verify SQLite and semantic ledger invariants.
futurediff-audit --root ~/.futurediff

# Inspect a pruning plan; no files are deleted.
futurediff-prune --root ~/.futurediff --older-than 720h

# Check local readiness without exposing secret values.
futurediff-doctor --root ~/.futurediff

# Compare this client with a running daemon API contract.
futurediff-api-contract --socket ~/.futurediff/futurediff.sock
```


## Forensic export and recovery

```bash
# Export one durable transaction and its exact patch when available.
futurediff-export --root ~/.futurediff \
  --transaction <transaction-id> \
  --output transaction.futurepack

# Verify without extracting.
futurediff-export --verify transaction.futurepack

# Replay the event log against current projections.
futurediff-replay --root ~/.futurediff --transaction <transaction-id>

# Validate a ledger backup without applying it.
futurediff-restore --root ~/.futurediff \
  --backup ~/backups/futurediff-ledger.db \
  --expected-sha256 <digest>

# Apply only while the daemon is stopped.
futurediff-restore --root ~/.futurediff \
  --backup ~/backups/futurediff-ledger.db \
  --expected-sha256 <digest> \
  --apply --confirm RESTORE_FUTUREDIFF_LEDGER
```

## Configuration and API compatibility

```bash
futurediff-config-lint --kind verification examples/verification/basic.verify.json
futurediff-config-lint --kind opencode ./opencode.futurediff.json
futurediff-api-diff --baseline examples/api/contract-v1.json
```

API authority changes are treated as incompatible, including any change to an endpoint's `agent_safe` classification.


## Adapter conformance and policy explanation

```bash
futurediff-effectspec --self-test
futurediff-policy-explain --contract examples/verification/basic.verify.json --assume-pass
```

## Recovery drill

```bash
futurediff-recovery-drill
```

The built-in scenarios prove that ambiguous provider outcomes never authorize a blind retry.

## Aggregate metrics and support bundle

```bash
futurediff-metrics --root ~/.futurediff --format prometheus
futurediff-support-bundle --root ~/.futurediff --output support.zip
futurediff-support-bundle --verify support.zip
```

Metrics are aggregate-only. Support bundles exclude ledger bytes, transaction patches, and credential configuration contents.

## Signed operator approvals

```bash
futurediff-approval generate \
  --approver operator@example.com \
  --private ~/.futurediff/operator-private.json \
  --keyring ~/.futurediff/operator-keyring.json

futurediff-approval sign \
  --private ~/.futurediff/operator-private.json \
  --transaction <transaction-id> \
  --digest <approval-material-digest> \
  --output approval.json

futurediff approve-signed <transaction-id> approval.json
```

Start the daemon with `--approval-keyring` and `--require-signed-approvals` to reject unsigned approval requests.

## Policy bundle, diff, and upgrade rehearsal

```bash
futurediff-policy-bundle \
  --contract examples/verification/basic.verify.json \
  --policy-id default-safe \
  --output default.fdpolicy

futurediff-diff --root ~/.futurediff \
  --transaction <transaction-id> --format markdown

futurediff-upgrade-rehearsal --root ~/.futurediff
futurediff-compat --manifest examples/compatibility.manifest.json
```
