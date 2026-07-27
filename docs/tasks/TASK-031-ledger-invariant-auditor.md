# Task 031 — Ledger invariant auditor

## Command

```bash
futurediff-audit --root ~/.futurediff
```

## Checks

- SQLite integrity and migration state
- Per-transaction event hash chains
- Approval digest/material-revision binding
- Committed repository-result presence
- Committed effect receipt presence
- Receipt/effect-state consistency
- Unknown-effect reconciliation state
- Terminal transactions with live effects
- Effect-dependency cycles
- Unresolved transactions older than 24 hours

The command is read-only and exits nonzero on errors. `--strict-warnings` also fails on warnings.
