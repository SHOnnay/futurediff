# Task 022 — Ledger Fault Injection and Corruption Checks

## Delivered

- Internal SQLite fault-injector interface
- Rollback when a fault occurs immediately before COMMIT
- Fault-safe backup behavior
- Corrupted-backup rejection
- Executable administrator self-test:

```bash
futurediff-admin --root /tmp/fd-admin --fault-self-test /tmp/fd-faults
```

## Executed result

```text
commit_failure_rolls_back: true
backup_failure_preserves_existing_artifact: true
corrupted_backup_is_rejected: true
```

The self-test uses a disposable directory and does not inject faults into the production ledger.
