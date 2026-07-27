CREATE TABLE api_idempotency_requests (
    principal_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    request_digest TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('in_progress','completed')),
    status_code INTEGER,
    response_content_type TEXT,
    response_body BLOB,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (principal_id, idempotency_key)
);

CREATE TABLE api_access_events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    principal_id TEXT NOT NULL,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    idempotency_key_digest TEXT,
    request_digest TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_api_access_created
ON api_access_events(created_at, sequence);
