ALTER TABLE users ADD COLUMN monthly_quota_bytes BIGINT CHECK (monthly_quota_bytes > 0),
    ADD COLUMN bandwidth_mbps INTEGER NOT NULL DEFAULT 3 CHECK (bandwidth_mbps BETWEEN 1 AND 3);

ALTER TABLE nodes ADD COLUMN monthly_quota_bytes BIGINT CHECK (monthly_quota_bytes > 0),
    ADD COLUMN bandwidth_mbps INTEGER CHECK (bandwidth_mbps BETWEEN 1 AND 100000);

CREATE FUNCTION node_monthly_quota_available(target UUID, quota BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT quota IS NULL OR (
        SELECT COALESCE(sum(bytes_up) + sum(bytes_down), 0) < quota
        FROM node_usage_daily WHERE node_id = target
        AND usage_day >= date_trunc('month', now() AT TIME ZONE 'UTC')::date
        AND usage_day <= (now() AT TIME ZONE 'UTC')::date
    )
$$;
