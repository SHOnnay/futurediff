# Contributing to FutureDiff

## Scope and safety model

FutureDiff is a local-first, cooperative CLI that reviews, verifies, and publishes AI-assisted changes. It is not a sandbox, not an agent supervisor, and not a hosted service. Contributions must preserve the safety guarantees: no silent branch mutation, no approval reuse, no credential leakage, no automatic merge, no Windows runtime claim.

## Supported platforms

- **Linux** (amd64, arm64) — fully supported
- **macOS** (arm64, Intel) — fully supported
- **Windows** — explicitly unsupported; no runtime support, no installer, no CI validation. PRs adding Windows-only code will be rejected unless they include a complete native validation story.

## Prerequisites

- Go 1.23+ (see `go.mod` for exact version)
- `sqlite3` development headers (`libsqlite3-dev` on Debian/Ubuntu, `sqlite3` on macOS via Homebrew)
- `make` for the build pipeline
- Git 2.30+

Configuration files in the repository (`config/*.json`) define the exact versions and policies used by CI.

## Isolated `FDIF_HOME`

Every test and manual run must use an isolated `FDIF_HOME` (or `--home` / `--root`). The default `~/.futurediff` is never used by CI. Set `FDIF_HOME` to a disposable directory (e.g., `$(mktemp -d)`). Example:

```bash
export FDIF_HOME=$(mktemp -d)
chmod 700 "$FDIF_HOME"
fdif doctor
```

## Disposable repositories for destructive tests

Tests that mutate the ledger, simulate corruption, or exercise restore must use a fresh repository created in a temp directory. Never run destructive operations against a real development home. The test fixtures in `internal/ledgerrestore/restore_test.go` and `internal/guidedcli/*_test.go` demonstrate the pattern.

## Dedicated branches and conventional commits

- Work on a dedicated branch: `hardening/<topic>`, `fix/<topic>`, `feat/<topic>`, `docs/<topic>`.
- Use conventional commits: `hardening(restore): ...`, `docs(readiness): ...`, `fix(lock): ...`, `test(probe): ...`.
- No direct commits to `main`. No force-push, no rebase of shared history, no tags, no releases, no auto-merge.

## Focused tests and regression coverage

- Run only the relevant package tests during development: `go test ./internal/ledgerrestore/...`, `go test ./internal/daemonlock/...`, etc.
- For race detection: `go test -race ./internal/...` (selected packages).
- Every bug fix must include a regression test. Every new feature must include tests covering the happy path and the fail-closed paths.

## Race testing

- `go test -race ./...` must pass on CI. Run locally before pushing if you modified shared-state code (locks, audit trail, SQLite operations, storage writes).

## Storage changes require fault-injection tests

Any change to the storage layer (`internal/storageguard`, `internal/durablewrite`, `internal/ledgerrestore` write paths) must add a fault-injection test case using the `Before(operation)` injector pattern. The fault types are: `create_failure`, `write_failure`, `short_write`, `sync_failure`, `dir_sync_failure`, `rename_failure`, `sqlite_full_disk`, `audit_write_failure`, `receipt_write_failure`, `git_write_failure`. Run with `go test -run 'Test.*Fault' ./internal/storageguard/...` etc.

## Provider effects require idempotency and reconciliation tests

Any new provider effect or change to `internal/app/external_effects.go` must include tests proving:
- Idempotency key durability and uniqueness
- Receipt persistence and verification
- Reconciliation behavior when provider state is unknown (`needs_reconciliation`)

## Git changes must prove default/source branch isolation

Changes to Git operations (`internal/guidedcli/git*.go`, `internal/app/git.go`) must include tests proving:
- The current branch is never mutated
- The default branch is never mutated
- Worktree isolation is maintained
- Branch naming is `futurediff/<transaction-id>` and create-only

## Approval changes must prevent approval reuse after material changes

Any change to approval logic (`internal/guidedcli/approval.go`, `internal/app/approval.go`) must include a test proving that an approval bound to a specific commit/digest is rejected after the commit changes.

## Recovery changes must preserve durable evidence

Any change to recovery (`internal/guidedcli/recover*.go`, `internal/app/recovery.go`, `internal/ledgerrestore/restore.go`) must ensure:
- Quarantine directories are never auto-deleted
- Evidence manifests (`evidence.json`) are written with fsync before directory sync
- The corrupt original (ledger, WAL, SHM) is preserved byte-for-byte
- No secret, credential, token, or raw env value appears in JSON output, error messages, evidence, or audit trail

## Secret-free JSON, errors, evidence, and audit

- All JSON output, error messages, evidence files, and audit records must be free of secrets (tokens, keys, passwords, raw env values, credential paths).
- Run `./scripts/secrets-scan.sh` on any new evidence artifacts before committing.
- The certification drill enforces this automatically.

## Documentation and manifest regeneration

- Update relevant docs when changing user-facing behavior (CLI flags, JSON fields, reason codes, new commands).
- Regenerate `MANIFEST.sha256` after adding/removing tracked files: `shasum -a 256 $(git ls-files) > MANIFEST.sha256`.
- ADR updates go in `docs/adr/` with the next sequential number.

## Full validation before PR readiness

Before marking a PR ready for review, run the full validation suite:

```bash
git diff --check
gofmt -l .
go test ./...
go test -race ./internal/...  # selected packages at minimum
go vet ./...
make check
make public-alpha-test
make build-public
./scripts/validate-macos-peer-auth.sh
./scripts/validate-overlay.sh
./scripts/certify-corruption-lock-disk-pressure.sh
shasum -a 256 -c MANIFEST.sha256
```

Packaging must produce valid binaries for: linux-amd64, linux-arm64, darwin-arm64, darwin-amd64. No flaky tests. No duplicate provider/Git effects. No credentials/secrets in any output. No untracked build artifacts.

## PR checklist

- [ ] Conventional commit messages on a dedicated branch
- [ ] Regression tests for any bug fix
- [ ] Tests covering new fail-closed paths
- [ ] Race detector clean on affected packages
- [ ] Fault-injection tests for storage changes
- [ ] Idempotency/reconciliation tests for provider changes
- [ ] Default/source branch isolation tests for Git changes
- [ ] Approval reuse prevention tests for approval changes
- [ ] Durable evidence preservation for recovery changes
- [ ] Secret scan on new evidence/JSON output
- [ ] Relevant docs updated (README, ADR, command reference, guided CLI, threat model, limitations, readiness)
- [ ] MANIFEST.sha256 regenerated if file set changed
- [ ] Full validation suite passes locally
- [ ] No direct-main mutation, no force-push, no auto-merge, no tags, no releases
- [ ] No Windows runtime support claims

## Private security reporting

Report security issues privately via the process in `SECURITY.md`. Do not open public issues for credential leaks, vulnerability details, or exploit demonstrations.

## No unsupported claims

- Do not claim Windows runtime support.
- Do not claim support for providers not in the repository.
- Do not claim rollback capability for authoritative state.
- Do not claim stable readiness without the complete evidence set documented in `STABLE_READINESS.md`.

---

See also: `README.md` (Getting Started), `docs/FDIF_GUIDED_CLI.md`, `docs/FDIF_COMMAND_REFERENCE.md`, `docs/adr/ADR-099-corruption-lock-disk-pressure-resilience.md`, `SECURITY.md`.