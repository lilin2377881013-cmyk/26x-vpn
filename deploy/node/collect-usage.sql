\set ON_ERROR_STOP on
BEGIN;
SELECT pg_advisory_xact_lock(260014, hashtext(:'node'));
WITH incoming AS (
    SELECT n.id AS node_id, sample.email AS label, metric.direction, metric.bytes
    FROM nodes n
    CROSS JOIN jsonb_to_recordset(:'samples'::jsonb) AS sample(email TEXT, up BIGINT, down BIGINT)
    CROSS JOIN LATERAL (VALUES ('up', sample.up), ('down', sample.down)) AS metric(direction, bytes)
    WHERE n.code = :'node' AND metric.bytes >= 0
), deltas AS (
    SELECT i.*,
        CASE WHEN c.bytes IS NULL THEN 0
             WHEN i.bytes < c.bytes THEN i.bytes
             ELSE i.bytes - c.bytes END AS delta
    FROM incoming i
    LEFT JOIN node_traffic_counters c USING (node_id, label, direction)
), node_totals AS (
    INSERT INTO node_usage_daily(node_id, usage_day, bytes_up, bytes_down)
    SELECT node_id, (now() AT TIME ZONE 'UTC')::date,
        COALESCE(sum(delta) FILTER (WHERE direction = 'up'), 0),
        COALESCE(sum(delta) FILTER (WHERE direction = 'down'), 0)
    FROM deltas GROUP BY node_id HAVING sum(delta) > 0
    ON CONFLICT (node_id, usage_day) DO UPDATE
    SET bytes_up = node_usage_daily.bytes_up + EXCLUDED.bytes_up,
        bytes_down = node_usage_daily.bytes_down + EXCLUDED.bytes_down
), mapped AS (
    SELECT x.*, d.id AS device_id, d.user_id
    FROM deltas x JOIN nodes n ON n.id = x.node_id
    CROSS JOIN LATERAL (
        SELECT min(device.id::text)::uuid AS id
        FROM devices device
        WHERE x.label = '26x-device-' || device.id::text
           OR x.label = 'device-' || left(device.id::text, 8) || '@' || n.code
        HAVING count(*) = 1
    ) matched
    JOIN devices d ON d.id = matched.id
), user_totals AS (
    INSERT INTO usage_daily(user_id, device_id, usage_day, bytes_up, bytes_down)
    SELECT user_id, device_id, (now() AT TIME ZONE 'UTC')::date,
        COALESCE(sum(delta) FILTER (WHERE direction = 'up'), 0),
        COALESCE(sum(delta) FILTER (WHERE direction = 'down'), 0)
    FROM mapped GROUP BY user_id, device_id HAVING sum(delta) > 0
    ON CONFLICT (device_id, usage_day) DO UPDATE
    SET bytes_up = usage_daily.bytes_up + EXCLUDED.bytes_up,
        bytes_down = usage_daily.bytes_down + EXCLUDED.bytes_down
)
INSERT INTO node_traffic_counters(node_id, label, direction, bytes)
SELECT node_id, label, direction, bytes FROM incoming
ON CONFLICT (node_id, label, direction) DO UPDATE SET bytes = EXCLUDED.bytes;
COMMIT;
