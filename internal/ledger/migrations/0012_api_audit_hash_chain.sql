ALTER TABLE api_access_events ADD COLUMN previous_digest TEXT;
ALTER TABLE api_access_events ADD COLUMN event_digest TEXT;
CREATE UNIQUE INDEX idx_api_access_event_digest ON api_access_events(event_digest) WHERE event_digest IS NOT NULL;
