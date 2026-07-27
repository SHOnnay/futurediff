CREATE TABLE migration_artifacts (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    sha256 TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);

CREATE TABLE ledger_backups (
    backup_id TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    created_at TEXT NOT NULL
);
