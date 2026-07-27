CREATE TABLE adapter_identities (
    adapter_id TEXT PRIMARY KEY,
    version TEXT NOT NULL,
    trust_level TEXT NOT NULL CHECK (trust_level IN ('built_in','verified','untrusted')),
    executable_digest TEXT,
    enabled INTEGER NOT NULL CHECK (enabled IN (0,1)),
    registered_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE credential_bindings (
    credential_id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    account TEXT,
    source_kind TEXT NOT NULL,
    source_reference_digest TEXT NOT NULL,
    allowed_adapters_json TEXT NOT NULL,
    allowed_operations_json TEXT NOT NULL,
    allowed_destinations_json TEXT NOT NULL,
    expires_at TEXT,
    enabled INTEGER NOT NULL CHECK (enabled IN (0,1)),
    registered_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE credential_access_events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    transaction_id TEXT,
    effect_id TEXT,
    adapter_id TEXT NOT NULL,
    credential_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    destination TEXT NOT NULL,
    decision TEXT NOT NULL CHECK (decision IN ('granted','denied','error')),
    reason TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_credential_access_transaction
ON credential_access_events(transaction_id, sequence);

CREATE INDEX idx_credential_access_credential
ON credential_access_events(credential_id, sequence);
