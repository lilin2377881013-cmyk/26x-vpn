ALTER TABLE nodes
    ADD COLUMN ssh_port INTEGER NOT NULL DEFAULT 22 CHECK (ssh_port BETWEEN 1 AND 65535),
    ADD COLUMN ssh_user TEXT NOT NULL DEFAULT 'root';

CREATE TABLE node_usage_daily (
    node_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    usage_day DATE NOT NULL,
    bytes_up BIGINT NOT NULL DEFAULT 0 CHECK (bytes_up >= 0),
    bytes_down BIGINT NOT NULL DEFAULT 0 CHECK (bytes_down >= 0),
    PRIMARY KEY (node_id, usage_day)
);

CREATE INDEX usage_daily_user_day_idx ON usage_daily(user_id, usage_day);
