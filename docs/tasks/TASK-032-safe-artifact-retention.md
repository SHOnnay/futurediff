# Task 032 — Safe artifact retention

## Workflow

```bash
futurediff-prune --root ~/.futurediff --older-than 720h
futurediff-prune --root ~/.futurediff --older-than 720h \
  --apply --confirm PRUNE_TERMINAL_FUTUREDIFF_ARTIFACTS
```

## Guarantees

- Only committed, aborted, or compensated transactions are candidates.
- Runtime paths must remain below `<data-root>/runtime/transactions`.
- Git worktrees are removed through the existing hardened staging manager.
- Published FutureDiff refs and durable ledger metadata are preserved.
- Applied actions record bytes removed and the exact plan digest.
- Dry-run is the default.
