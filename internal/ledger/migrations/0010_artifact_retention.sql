CREATE TABLE artifact_retention_actions (
    action_id TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    runtime_root TEXT NOT NULL,
    bytes_removed INTEGER NOT NULL CHECK (bytes_removed >= 0),
    plan_digest TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_artifact_retention_transaction
ON artifact_retention_actions(transaction_id);
