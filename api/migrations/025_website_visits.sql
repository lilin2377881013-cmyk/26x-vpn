CREATE TABLE website_visits (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_key text NOT NULL UNIQUE,
    visited_at timestamptz NOT NULL,
    page text NOT NULL,
    referral_code text,
    device text NOT NULL,
    os text NOT NULL,
    browser text NOT NULL
);
CREATE INDEX website_visits_time_idx ON website_visits (visited_at DESC, id DESC);
