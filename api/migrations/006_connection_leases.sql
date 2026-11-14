CREATE TABLE connection_leases (
    device_id UUID PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL,
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX connection_leases_account_idx ON connection_leases(user_id, expires_at);
