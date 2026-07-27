CREATE TABLE verification_runs (
    verification_id TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    contract_id TEXT NOT NULL,
    contract_digest TEXT NOT NULL,
    material_digest TEXT NOT NULL,
    material_revision INTEGER NOT NULL CHECK (material_revision >= 0),
    outcome TEXT NOT NULL CHECK (outcome IN ('pass', 'fail', 'error')),
    verification_digest TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(transaction_id, verification_digest)
);

CREATE INDEX idx_verification_runs_transaction_created
    ON verification_runs(transaction_id, created_at, verification_id);

CREATE TABLE verification_check_results (
    verification_id TEXT NOT NULL REFERENCES verification_runs(verification_id) ON DELETE CASCADE,
    check_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    required INTEGER NOT NULL CHECK (required IN (0, 1)),
    status TEXT NOT NULL CHECK (status IN ('pass', 'fail', 'error', 'timeout', 'blocked', 'cancelled')),
    cache_hit INTEGER NOT NULL CHECK (cache_hit IN (0, 1)),
    check_spec_digest TEXT NOT NULL,
    cache_key TEXT NOT NULL,
    evidence_digest TEXT NOT NULL,
    stdout_ref TEXT,
    stderr_ref TEXT,
    execution_evidence_ref TEXT,
    message TEXT,
    PRIMARY KEY (verification_id, check_id),
    UNIQUE(verification_id, ordinal)
);

CREATE INDEX idx_verification_check_results_status
    ON verification_check_results(verification_id, required, status);
