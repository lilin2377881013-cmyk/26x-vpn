CREATE TABLE node_traffic_counters (
    node_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('up', 'down')),
    bytes BIGINT NOT NULL CHECK (bytes >= 0),
    PRIMARY KEY (node_id, label, direction)
);
