# FutureDiff Tasks 011–013 Validation Report

**Date:** 2026-07-27  
**Overall result:** PASS for all checks executable in this environment

## Toolchain

```text
Go 1.23.2
Linux amd64
Git available
SQLite system library available
```

## Results

```text
gofmt                           PASS
go vet ./...                    PASS
go test ./...                   PASS
go test -race ./...             PASS
go test -cover ./...            PASS
futurediff build                PASS
futurediffd build               PASS
futurediff-mcp build            PASS
provider credential JSON        PASS
MCP client JSON                 PASS
Unix-socket daemon smoke        PASS
MCP process smoke               PASS
socket permission 0600          PASS
privileged MCP tools absent     PASS
```

## Coverage snapshot

| Package | Coverage |
|---|---:|
| Credential broker | 72.3% |
| OCI runtime | 69.3% |
| Slack outbox | 65.9% |
| Application orchestration | 64.3% |
| Git staging | 63.7% |
| GitHub draft PR | 61.4% |
| FuturePack | 61.9% |
| EffectSpec | 55.6% |
| Local API | 54.0% |
| Verification | 51.3% |
| Domain | 47.4% |
| MCP bridge | 43.4% |
| GitHub branch publication | 42.7% |
| Ledger | 26.1% |

The ledger remains the main test-coverage weakness.

## Not certified here

- real GitHub remote branch creation;
- real GitHub draft PR creation;
- real Slack message release;
- rootless Docker execution;
- rootless Podman execution;
- macOS and Windows builds;
- production secret-manager integration.

Those require dedicated external test environments and should not be inferred from fake-provider or source-level tests.
