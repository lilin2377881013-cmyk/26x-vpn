CREATE TABLE referral_links (
    code TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE referral_daily (
    code TEXT NOT NULL REFERENCES referral_links(code),
    day DATE NOT NULL,
    device TEXT NOT NULL,
    channel TEXT NOT NULL,
    count BIGINT NOT NULL DEFAULT 1 CHECK (count > 0),
    PRIMARY KEY (code, day, device, channel)
);
