CREATE TABLE effect_documents (
    effect_id TEXT PRIMARY KEY REFERENCES effects(effect_id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    destination TEXT NOT NULL,
    input_json TEXT NOT NULL,
    prepared_json TEXT NOT NULL,
    prepared_digest TEXT NOT NULL,
    preview_json TEXT NOT NULL,
    preview_digest TEXT NOT NULL,
    resource_versions_json TEXT NOT NULL,
    support_level TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE effect_attempts (
    attempt_id TEXT PRIMARY KEY,
    effect_id TEXT NOT NULL REFERENCES effects(effect_id) ON DELETE CASCADE,
    transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    phase TEXT NOT NULL CHECK (phase IN ('commit','status','abort','compensate')),
    request_digest TEXT NOT NULL,
    fencing_token INTEGER NOT NULL CHECK (fencing_token > 0),
    outcome TEXT NOT NULL CHECK (outcome IN ('intent','success','definite_failure','unknown','not_found')),
    http_status INTEGER,
    response_digest TEXT,
    error_class TEXT,
    error_message TEXT,
    started_at TEXT NOT NULL,
    finished_at TEXT
);

CREATE INDEX idx_effect_attempts_effect_started
ON effect_attempts(effect_id, started_at, attempt_id);

CREATE INDEX idx_effect_documents_destination
ON effect_documents(destination, operation);
