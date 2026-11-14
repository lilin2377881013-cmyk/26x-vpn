CREATE TABLE trial_accounts (
    installation_hash BYTEA PRIMARY KEY,
    user_id UUID NOT NULL UNIQUE REFERENCES users(id),
    code TEXT NOT NULL UNIQUE
);

INSERT INTO plans (code, name, duration_days, max_devices, tier, active)
VALUES ('trial-15m', '15 分钟试用', 1, 1, 'standard', false);
