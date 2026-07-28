CREATE TABLE authorization_decisions (
  sequence INTEGER PRIMARY KEY,
  decision_id TEXT NOT NULL UNIQUE,
  principal_id TEXT NOT NULL,
  operation_id TEXT NOT NULL,
  resource_id TEXT,
  allowed INTEGER NOT NULL,
  source TEXT NOT NULL,
  reason_code TEXT NOT NULL,
  policy_digest TEXT,
  role_names TEXT,
  capability_digest TEXT,
  request_id TEXT,
  created_at TEXT NOT NULL,
  previous_digest TEXT,
  event_digest TEXT NOT NULL UNIQUE
);
CREATE INDEX idx_authorization_decisions_principal ON authorization_decisions(principal_id, created_at);
CREATE INDEX idx_authorization_decisions_operation ON authorization_decisions(operation_id, created_at);

CREATE TABLE authorization_capability_uses (
  capability_id TEXT PRIMARY KEY,
  principal_id TEXT NOT NULL,
  operation_id TEXT NOT NULL,
  resource_id TEXT,
  capability_digest TEXT NOT NULL,
  used_at TEXT NOT NULL
);
