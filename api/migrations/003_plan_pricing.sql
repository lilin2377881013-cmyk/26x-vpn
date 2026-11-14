ALTER TABLE plans
    ADD COLUMN price_cents BIGINT CHECK (price_cents IS NULL OR price_cents > 0),
    ADD COLUMN currency TEXT NOT NULL DEFAULT 'CNY'
        CHECK (currency ~ '^[A-Z]{3}$');

INSERT INTO plans (code, name, tier, duration_days, price_cents, currency, quota_bytes, max_devices, active)
VALUES
    ('standard-30d',  '普通档 30 天',  'standard', 30,  3000, 'CNY', NULL, 1, TRUE),
    ('standard-90d',  '普通档 90 天',  'standard', 90,  7500, 'CNY', NULL, 1, TRUE),
    ('standard-365d', '普通档 365 天', 'standard', 365, 22500, 'CNY', NULL, 1, TRUE),
    ('premium-30d',   '豪华档 30 天',  'premium',  30,  6000, 'CNY', NULL, 1, TRUE),
    ('premium-90d',   '豪华档 90 天',  'premium',  90, 15000, 'CNY', NULL, 1, TRUE),
    ('premium-365d',  '豪华档 365 天', 'premium', 365, 50000, 'CNY', NULL, 1, TRUE)
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    tier = EXCLUDED.tier,
    duration_days = EXCLUDED.duration_days,
    price_cents = EXCLUDED.price_cents,
    currency = EXCLUDED.currency,
    active = TRUE;
