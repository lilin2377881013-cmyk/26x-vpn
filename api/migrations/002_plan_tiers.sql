ALTER TABLE plans
    ADD COLUMN tier TEXT NOT NULL DEFAULT 'standard'
    CHECK (tier IN ('standard', 'premium'));
