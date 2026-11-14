ALTER TABLE connection_leases
    ADD COLUMN endpoint_id UUID REFERENCES node_endpoints(id) ON DELETE CASCADE;

CREATE INDEX connection_leases_endpoint_expiry_idx
    ON connection_leases(endpoint_id, expires_at)
    WHERE endpoint_id IS NOT NULL;
