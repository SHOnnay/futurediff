# FutureDiff

FutureDiff is a transactional effect layer for autonomous AI agents.

> Existing agents decide what to do. FutureDiff controls whether and how those decisions become persistent reality.

## Current milestone: Tasks 011–013

The canonical implementation is Go and now supports this staged transaction chain:

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

The build produces:

```text
bin/futurediff
bin/futurediffd
bin/futurediff-mcp
```

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

## Operating modes

### Cooperative

Provides durable staging and provider-effect orchestration, but does not claim every host-side bypass path is prevented.

### Enforced

Requires a rootless OCI runtime and digest-pinned image. Agent-authored commands run without provider credentials, with network disabled by default, and without mounting the live checkout or Git metadata.

Real rootless-host certification remains pending.

## Documentation

- `ARCHITECTURE.md`
- `docs/tasks/TASK-011-controlled-github-branch-publication.md`
- `docs/tasks/TASK-012-slack-durable-outbox.md`
- `docs/tasks/TASK-013-generic-mcp-bridge.md`
- `docs/progress/PROGRESS-AUDIT-TASK-013.md`
- `docs/daemon/api-v0.5.md`

## Current progress estimate

- architecture and research: **98%**;
- narrow open-source MVP: **78%**;
- production-grade platform: **46%**.

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

A matching manual GitHub Actions workflow exists at `.github/workflows/rootless-certification.yml` for self-hosted Linux runners that actually provide Docker-rootless or Podman-rootless.

Or use the helper script on a Linux host that already has a rootless runtime available:

```bash
export FUTUREDIFF_TEST_IMAGE='alpine@sha256:<64-hex-digest>'
make rootless-certify-live
```

Set `FUTUREDIFF_RUNTIME=podman` to certify Podman instead of Docker, and set `FUTUREDIFF_RUNTIME_BINARY` when the runtime binary is not on `PATH`.


## Real provider smoke certification

```bash
export FUTUREDIFF_GITHUB_TOKEN='test-or-production-token'
export FUTUREDIFF_SLACK_TOKEN='test-or-production-token'

futurediff-certify-providers \
  --output provider-certification.json \
  --futurepack provider-certification.futurepack \
  --github-owner acme \
  --github-repo app \
  --github-base main \
  --github-expected-sha <stale-sha> \
  --slack-channel C0123456789
```

This smoke certification checks that GitHub freshness detects the stale expected SHA and that Slack posting plus status recovery succeed in the controlled test environment.

Or run the helper script with environment variables:

```bash
export FUTUREDIFF_GITHUB_TOKEN='test-or-production-token'
export FUTUREDIFF_SLACK_TOKEN='test-or-production-token'
export FUTUREDIFF_GITHUB_OWNER=acme
export FUTUREDIFF_GITHUB_REPO=app
export FUTUREDIFF_GITHUB_BASE=main
export FUTUREDIFF_GITHUB_EXPECTED_SHA=<stale-sha>
export FUTUREDIFF_SLACK_CHANNEL=C0123456789

make provider-certify-live
```

Set `FUTUREDIFF_PROVIDER_CERT_KEEP_ROOT=true` to keep the generated JSON, futurepack, and artifact directory instead of deleting the temp root after a successful run.

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
