ALTER TABLE api_access_events ADD COLUMN request_id TEXT;
CREATE INDEX IF NOT EXISTS idx_api_access_request_id ON api_access_events(request_id);
