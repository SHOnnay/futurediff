# ADR-057: Diagnostics do not display secrets

`futurediff-doctor` reports platform, file permissions, Git, SQLite, ledger invariants, daemon health, credential-file permissions, and optional rootless runtime readiness. It never resolves or prints provider secret values.
