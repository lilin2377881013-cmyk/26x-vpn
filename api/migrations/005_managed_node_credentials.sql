ALTER TABLE device_node_credentials
    ADD COLUMN managed BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN reconcile_after TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE INDEX managed_credentials_due_idx
    ON device_node_credentials(endpoint_id, reconcile_after)
    WHERE managed AND status IN ('pending', 'active', 'revoking');
