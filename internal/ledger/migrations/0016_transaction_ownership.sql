ALTER TABLE transactions ADD COLUMN owner_principal_id TEXT NOT NULL DEFAULT 'legacy:unowned';
CREATE INDEX idx_transactions_owner_principal ON transactions(owner_principal_id, updated_at);

CREATE TABLE transaction_access_grants (
  transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
  principal_id TEXT NOT NULL,
  permission TEXT NOT NULL CHECK(permission IN ('read','operate')),
  granted_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(transaction_id, principal_id)
);
CREATE INDEX idx_transaction_access_principal ON transaction_access_grants(principal_id, transaction_id);

CREATE TABLE transaction_access_events (
  sequence INTEGER PRIMARY KEY,
  event_id TEXT NOT NULL UNIQUE,
  transaction_id TEXT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
  actor_principal_id TEXT NOT NULL,
  subject_principal_id TEXT NOT NULL,
  action TEXT NOT NULL CHECK(action IN ('created','granted','revoked')),
  permission TEXT,
  request_id TEXT,
  created_at TEXT NOT NULL,
  previous_digest TEXT,
  event_digest TEXT NOT NULL UNIQUE
);
CREATE INDEX idx_transaction_access_events_tx ON transaction_access_events(transaction_id, sequence);
