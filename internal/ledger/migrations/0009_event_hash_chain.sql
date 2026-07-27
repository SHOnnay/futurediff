ALTER TABLE events ADD COLUMN previous_event_hash TEXT;
ALTER TABLE events ADD COLUMN event_hash TEXT;

CREATE UNIQUE INDEX idx_events_event_hash
ON events(event_hash)
WHERE event_hash IS NOT NULL;
