ALTER TABLE sessions ADD COLUMN device_id UUID REFERENCES devices(id);
CREATE INDEX sessions_live_device_idx ON sessions(device_id) WHERE revoked_at IS NULL AND device_id IS NOT NULL;

ALTER TABLE devices ADD COLUMN last_login_at TIMESTAMPTZ;
-- Older clients recorded device activity but had no separate login timestamp.
UPDATE devices SET last_login_at=COALESCE(last_seen_at,created_at);
ALTER TABLE devices ALTER COLUMN last_login_at SET DEFAULT now();
ALTER TABLE devices ALTER COLUMN last_login_at SET NOT NULL;
