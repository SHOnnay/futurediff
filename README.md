# FutureDiff

FutureDiff is a local-first transaction and safety layer for AI agents. It stages repository and provider changes in isolation, verifies exact material, binds approval to cryptographic digests, and releases effects only through controlled, recoverable adapters.

## Status

- Baseline: **v0.95.0**
- Local API contract: **v1.1**
- Commands: **70 Go binaries**
- Position: **feature-complete local MVP**
- Still pending: external Docker/Podman, GitHub, Slack, OpenCode, Hermes, macOS, and hosted-attestation certification

## Core guarantees

- exact Git-tree staging without mutating the live checkout
- digest-bound approval and deterministic verification
- rootless OCI execution for enforced preparation
- durable effect orchestration for GitHub branch, draft PR, and Slack release flows
- idempotent receipts, reconciliation, and crash recovery
- kernel-authenticated local ownership, sharing, RBAC, and access audit chains
- tamper-evident evidence, audit, and integrity checkpoints

## Requirements

Cooperative mode:
- Go 1.23+
- Git
- C compiler
- SQLite development library

Enforced mode additionally requires:
- rootless Docker or rootless Podman
- digest-pinned OCI image: `name@sha256:<digest>`

## Build and test

```bash
make build
go test ./...
```

Release archive:

```bash
bash ./scripts/release.sh
```

## Start the daemon

```bash
chmod 600 examples/credentials/providers.example.json
export FUTUREDIFF_GITHUB_TOKEN='test-or-production-token'
export FUTUREDIFF_SLACK_TOKEN='test-or-production-token'

./bin/futurediffd \
  --credential-config examples/credentials/providers.example.json
```

## Minimal transaction flow

```bash
./bin/futurediff create /path/to/repository cooperative
./bin/futurediff seal <transaction-id>
./bin/futurediff verify <transaction-id> examples/verification/basic.verify.json
./bin/futurediff approval-material <transaction-id>
./bin/futurediff approve <transaction-id> <transaction-digest>
./bin/futurediff commit <transaction-id> <transaction-digest>
```

Provider effects are prepared before commit through the GitHub branch, GitHub draft PR, and Slack commands.

## Documentation

- `ARCHITECTURE.md`
- `docs/progress/MASTER-STATUS-v0.95.0.md`
- `docs/progress/PROGRESS-AUDIT-TASK-095.md`
- `docs/tasks/TASK-091-095-validation.md`
- `docs/tasks/`
- `docs/adr/`

## Claims boundary

FutureDiff is ready as a serious local product baseline. It is **not** yet claiming external production certification, distributed HA, enterprise IAM, Windows support, or a production UI.
