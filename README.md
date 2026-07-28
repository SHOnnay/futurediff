# FutureDiff

FutureDiff is a transaction and safety layer for AI agents. It stages repository and provider changes in isolation, verifies exact material, binds approval to cryptographic digests, and releases effects only through controlled, recoverable adapters.

## Current state

- Core transaction baseline: **v0.95.0**
- Imported assurance/promotion/closure overlay: **through task 170**
- Local API contract: **v1.1**
- Commands: **70 Go binaries**
- Position: **strong local product baseline with production-assurance tooling present**
- External production completion: **not yet proven**

## What it guarantees

- exact Git-tree staging without mutating the live checkout
- digest-bound approval and deterministic verification
- rootless OCI preparation for enforced execution
- durable GitHub branch, draft PR, and Slack effect release flows
- idempotent receipts, reconciliation, and crash recovery
- kernel-authenticated local ownership, sharing, RBAC, and access audit chains
- tamper-evident evidence, audit, integrity, and retention controls
- imported promotion, launch, and production-closure assurance workflows

## Requirements

Cooperative mode:
- Go 1.23+
- Git
- C compiler
- SQLite development library

Enforced mode additionally requires:
- rootless Docker or rootless Podman
- digest-pinned OCI image: `name@sha256:<digest>`

## Build and verify

```bash
make build
go test ./...
bash ./scripts/release.sh
```

Imported assurance tooling:

```bash
bash ./scripts/validate-overlay.sh
python3 -m unittest discover -s tests -p 'test_*.py' -v
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

## Production-assurance and closure tooling

```bash
./scripts/operations-assurance.sh dist/operations
./scripts/release-promotion.sh <inputs...> dist/promotion
./scripts/production-launch.sh <inputs...> dist/launch
./scripts/production-closure.sh <canonical-repo> <base-archive> <historical-zips-dir> dist/closure
```

These tools are present and locally validated. They do **not** turn placeholder, synthetic, skipped, or locally authored evidence into an external production pass.

## Root docs

- `ARCHITECTURE.md`
- `SECURITY.md`

## Key docs

- `docs/progress/MASTER-STATUS-v1.70.0.md`
- `docs/progress/PROGRESS-AUDIT-TASK-170.md`
- `docs/PRODUCTION_RUNBOOK.md`
- `docs/RELEASE_PROMOTION.md`
- `docs/PRODUCTION_LAUNCH.md`
- `docs/PRODUCTION_CLOSURE.md`
- `docs/THREAT_MODEL.md`

## Claims boundary

FutureDiff is a serious local product baseline with imported assurance and closure machinery. It is **not yet externally production-complete** until real provider, runtime, hosted-platform, security-review, load/soak, disaster-recovery, deployment-smoke, rollback, and sign-off evidence all pass.