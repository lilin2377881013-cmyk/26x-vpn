ALTER TABLE users DROP CONSTRAINT users_bandwidth_mbps_check;
ALTER TABLE users ALTER COLUMN bandwidth_mbps SET DEFAULT 20;
UPDATE users SET bandwidth_mbps = 20;
ALTER TABLE users ADD CONSTRAINT users_bandwidth_mbps_check CHECK (bandwidth_mbps BETWEEN 1 AND 20);
