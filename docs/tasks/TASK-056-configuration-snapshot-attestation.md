# Task 056 — Configuration snapshot attestation

## Delivered
- `internal/configsnapshot`
- `futurediff-config-snapshot build`
- `futurediff-config-snapshot verify`
- Digest, mode, size, existence, and required/optional file checks
- Symlink and non-regular-file rejection
- Strict JSON and manifest tamper detection
- JSON Schema and example documentation

## Security boundary
Snapshot files contain no source configuration bytes. They may expose canonical file paths and should still be stored with mode `0600`.

## Validation
A valid snapshot verified successfully. A subsequent content change caused verification to exit nonzero.
