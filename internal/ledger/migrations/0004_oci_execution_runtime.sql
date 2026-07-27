CREATE TABLE runtime_executions (
    execution_id TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    purpose TEXT NOT NULL,
    command_digest TEXT NOT NULL,
    environment_digest TEXT NOT NULL,
    policy_digest TEXT NOT NULL,
    image TEXT NOT NULL,
    image_digest TEXT NOT NULL,
    runtime_kind TEXT NOT NULL,
    runtime_version TEXT NOT NULL,
    exit_code INTEGER NOT NULL,
    termination_reason TEXT NOT NULL,
    stdout_path TEXT,
    stderr_path TEXT,
    evidence_path TEXT NOT NULL UNIQUE,
    workspace_synchronized INTEGER NOT NULL CHECK (workspace_synchronized IN (0,1)),
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL
);

CREATE INDEX idx_runtime_executions_transaction
ON runtime_executions(transaction_id, started_at, execution_id);
