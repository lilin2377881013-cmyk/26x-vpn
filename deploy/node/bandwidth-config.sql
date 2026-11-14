SELECT jsonb_build_object(
    'nodeMbps', n.bandwidth_mbps,
    'users', COALESCE((
        SELECT jsonb_object_agg(labels.label, jsonb_build_object('account', u.id, 'mbps', u.bandwidth_mbps))
        FROM users u JOIN devices d ON d.user_id = u.id
        CROSS JOIN LATERAL (VALUES ('device-' || left(d.id::text, 8) || '@' || n.code),
                                  ('26x-device-' || d.id::text)) AS labels(label)
        WHERE EXISTS (
            SELECT 1 FROM device_node_credentials c JOIN node_endpoints e ON e.id=c.endpoint_id
            WHERE c.device_id=d.id AND e.node_id=n.id AND c.status='active' AND c.revoked_at IS NULL
        )
    ), '{}'::jsonb)
)
FROM nodes n WHERE n.code=:'node';
