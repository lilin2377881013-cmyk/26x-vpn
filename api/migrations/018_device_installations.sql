ALTER TABLE devices ADD COLUMN installation_token TEXT;
CREATE UNIQUE INDEX devices_active_installation_idx ON devices(user_id, installation_token)
    WHERE revoked_at IS NULL AND installation_token IS NOT NULL;
