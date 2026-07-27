CREATE TABLE transactions (
    transaction_id TEXT PRIMARY KEY,
    protocol_version TEXT NOT NULL,
    mode TEXT NOT NULL,
    agent_adapter TEXT,
    agent_session_id TEXT,
    workspace_identity TEXT,
    base_revision TEXT,
    status TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    material_revision INTEGER NOT NULL DEFAULT 0 CHECK (material_revision >= 0),
    approval_digest TEXT,
    created_at TEXT NOT NULL,
    sealed_at TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE effects (
    effect_id TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    tool_identity TEXT NOT NULL,
    adapter_identity TEXT NOT NULL,
    effect_class TEXT NOT NULL,
    risk_level TEXT,
    input_digest TEXT,
    prepared_handle_ref TEXT,
    idempotency_key TEXT,
    commit_request_digest TEXT,
    commit_fencing_token INTEGER CHECK (commit_fencing_token IS NULL OR commit_fencing_token > 0),
    status TEXT NOT NULL,
    preview_ref TEXT,
    reversibility TEXT NOT NULL,
    commit_rank INTEGER,
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(adapter_identity, idempotency_key)
);

CREATE INDEX idx_effects_transaction_status
    ON effects(transaction_id, status);

CREATE TABLE effect_dependencies (
    effect_id TEXT NOT NULL REFERENCES effects(effect_id) ON DELETE CASCADE,
    depends_on_effect_id TEXT NOT NULL REFERENCES effects(effect_id) ON DELETE CASCADE,
    PRIMARY KEY (effect_id, depends_on_effect_id),
    CHECK (effect_id <> depends_on_effect_id)
);

CREATE TABLE events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    effect_id TEXT REFERENCES effects(effect_id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    payload_digest TEXT NOT NULL,
    fencing_token INTEGER CHECK (fencing_token IS NULL OR fencing_token > 0),
    created_at TEXT NOT NULL
);

CREATE INDEX idx_events_transaction_sequence
    ON events(transaction_id, sequence);

CREATE INDEX idx_events_effect_sequence
    ON events(effect_id, sequence);

CREATE TABLE evidence (
    evidence_id TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    effect_id TEXT REFERENCES effects(effect_id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    content_digest TEXT NOT NULL,
    blob_ref TEXT NOT NULL,
    producer TEXT NOT NULL,
    sensitivity TEXT NOT NULL,
    redaction_state TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE resource_versions (
    resource_version_id TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    effect_id TEXT REFERENCES effects(effect_id) ON DELETE CASCADE,
    resource_identity TEXT NOT NULL,
    version_value TEXT NOT NULL,
    observed_at TEXT NOT NULL
);

CREATE TABLE verifications (
    verification_id TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    effect_id TEXT REFERENCES effects(effect_id) ON DELETE CASCADE,
    verifier_identity TEXT NOT NULL,
    result TEXT NOT NULL,
    evidence_digest TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE approvals (
    approval_id TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    transaction_digest TEXT NOT NULL,
    material_revision INTEGER NOT NULL CHECK (material_revision >= 0),
    approver_identity TEXT NOT NULL,
    scope TEXT NOT NULL,
    decision TEXT NOT NULL,
    signature_ref TEXT,
    created_at TEXT NOT NULL,
    expires_at TEXT
);

CREATE INDEX idx_approvals_transaction_created
    ON approvals(transaction_id, created_at);

CREATE TABLE receipts (
    receipt_id TEXT PRIMARY KEY,
    effect_id TEXT NOT NULL REFERENCES effects(effect_id) ON DELETE CASCADE,
    provider_operation_id TEXT,
    provider_resource_id TEXT,
    request_digest TEXT NOT NULL,
    response_digest TEXT,
    status_query_ref TEXT,
    fencing_token INTEGER NOT NULL CHECK (fencing_token > 0),
    committed_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE(effect_id)
);

CREATE TABLE compensations (
    compensation_id TEXT PRIMARY KEY,
    effect_id TEXT NOT NULL REFERENCES effects(effect_id) ON DELETE CASCADE,
    strategy TEXT NOT NULL,
    input_digest TEXT NOT NULL,
    status TEXT NOT NULL,
    receipt_ref TEXT,
    started_at TEXT NOT NULL,
    completed_at TEXT
);

CREATE TABLE leases (
    lease_name TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    fencing_token INTEGER NOT NULL CHECK (fencing_token > 0),
    acquired_at_ms INTEGER NOT NULL,
    expires_at_ms INTEGER NOT NULL,
    CHECK (expires_at_ms >= acquired_at_ms)
);
