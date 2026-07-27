CREATE TABLE transaction_expiry_actions (
    action_id TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id),
    prior_status TEXT NOT NULL,
    policy_digest TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

CREATE TABLE idempotency_gc_actions (
    action_id TEXT PRIMARY KEY,
    completed_deleted INTEGER NOT NULL,
    in_progress_deleted INTEGER NOT NULL,
    completed_before TEXT NOT NULL,
    in_progress_before TEXT NOT NULL,
    plan_digest TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

CREATE TABLE backup_retention_actions (
    action_id TEXT PRIMARY KEY,
    backup_id TEXT NOT NULL,
    path_digest TEXT NOT NULL,
    bytes_removed INTEGER NOT NULL,
    plan_digest TEXT NOT NULL,
    applied_at TEXT NOT NULL
);
