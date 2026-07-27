# ADR-046: Ledger fault injection is a release gate

Status: accepted

The SQLite bridge supports an internal fault injector used only by tests and the disposable administrator self-test. A failure immediately before COMMIT must roll back the transaction. Backup failure must not replace an existing artifact. Corrupted backups must fail open or integrity verification.

Production operation does not enable fault injection.
