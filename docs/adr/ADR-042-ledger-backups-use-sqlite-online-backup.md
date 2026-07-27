# ADR-042: Ledger backups use SQLite's online backup API

Status: accepted

Copying a live WAL-mode database file is not an acceptable backup strategy. `futurediff-admin` uses SQLite's online backup API, validates the resulting database with `integrity_check`, publishes the backup atomically, records its SHA-256 digest, and stores backup metadata in the ledger. Embedded migration files are also checksummed and verified on every open.
