# Tasks 036–040 Validation

## Automated checks

```text
gofmt                              PASS
go vet ./...                       PASS
go test ./...                      PASS
go test -race ./...                PASS
25 Go commands built               PASS
```

## Executable scenarios

```text
Committed demo transaction         PASS
Transaction futurepack export      PASS
Futurepack independent verify      PASS
Event projection replay            PASS
Ledger backup dry-run validation   PASS
Confirmation-gated ledger restore  PASS
Post-restore semantic audit        PASS
Verification config lint           PASS
Strict OpenCode config lint        PASS
Semantic API contract diff         PASS
```

External provider and OCI certification was not part of these local-only tasks.
