# FutureDiff Architecture

## Scope

FutureDiff is a local-first control plane for agent-authored changes. Agents can propose work. FutureDiff stages it, verifies it, binds approval to exact material, and releases persistent effects through trusted paths only.

## Trusted flow

```text
agent intent
  -> staged repository / staged provider effects
  -> exact diff and effect preview
  -> deterministic verification
  -> digest-bound approval
  -> ordered effect release
  -> durable receipts / reconciliation
  -> audit / evidence / retention
  -> promotion / launch / closure gates
```

## Core components

### Transaction kernel
- durable transaction state machine
- exact Git tree staging
- approval material and digest binding
- commit-only trusted release path

### Execution boundary
- cooperative staging for local review flows
- enforced rootless OCI preparation for isolated execution
- sanitized workspace and bounded environment handling

### Effect layer
- GitHub branch publication
- GitHub draft PR creation
- Slack notification / outbox flows
- idempotent release ordering and crash-safe reconciliation

### Ledger and evidence
- SQLite-backed durable ledger
- approvals, attempts, receipts, and audit chains
- evidence export, integrity checkpoints, retention, and restore tooling

### Local authorization
- kernel-derived local principal identity
- transaction ownership, scoped sharing, RBAC, and auditability
- no delegated approval or commit authority through normal agent interfaces

### Assurance layer
- supply-chain checks
- readiness, operations, promotion, launch, and closure policies
- deterministic Python-based assurance, promotion, and closure toolchains
- machine-readable evidence contracts and workflows

## Repository shape

```text
cmd/        Go daemon and CLI commands
internal/   transaction, ledger, runtime, policy, and adapter packages
config/     operational, promotion, and closure policies
docs/       tasks, runbooks, threat model, progress, and audits
examples/   non-authoritative policy-conformance examples
schemas/    machine-readable contracts
scripts/    build, release, assurance, promotion, and closure entrypoints
tools/      Python assurance, operations, promotion, and closure tools
tests/      Python assurance and closure test suites
```

## Non-claims

FutureDiff does not claim external production completion from synthetic or local-only evidence. Real runtime, provider, hosted-platform, security-review, load/soak, disaster-recovery, deployment-smoke, rollback, and operational sign-off evidence remain separate proof obligations.