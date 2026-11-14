-- 26x control-plane schema, PostgreSQL.
-- Do not store browsing destinations, DNS query history, or VPN payloads.

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email CITEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'deleted')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    duration_days INTEGER NOT NULL CHECK (duration_days > 0),
    quota_bytes BIGINT CHECK (quota_bytes IS NULL OR quota_bytes >= 0),
    max_devices SMALLINT NOT NULL CHECK (max_devices BETWEEN 1 AND 32),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    plan_id UUID NOT NULL REFERENCES plans(id),
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'expired', 'revoked')),
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at > starts_at)
);
CREATE INDEX subscriptions_user_status_idx ON subscriptions(user_id, status, ends_at);

CREATE TABLE devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    platform TEXT NOT NULL CHECK (platform IN ('ios', 'macos', 'windows', 'android')),
    label TEXT NOT NULL,
    app_version TEXT,
    core_version TEXT,
    last_seen_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX devices_user_live_idx ON devices(user_id, created_at) WHERE revoked_at IS NULL;

CREATE TABLE nodes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    region_code TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'provisioning' CHECK (status IN ('provisioning', 'online', 'degraded', 'offline', 'disabled')),
    capacity_limit INTEGER,
    health_checked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE node_endpoints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    protocol TEXT NOT NULL CHECK (protocol IN ('hysteria2', 'vless_reality')),
    host TEXT NOT NULL,
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    transport TEXT NOT NULL CHECK (transport IN ('tcp', 'udp')),
    server_name TEXT,
    public_key TEXT,
    short_id TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (node_id, protocol, host, port)
);

-- Secret values are envelope-encrypted by the control plane before insertion.
-- The master encryption key lives outside PostgreSQL and outside node metadata.
CREATE TABLE device_node_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    endpoint_id UUID NOT NULL REFERENCES node_endpoints(id) ON DELETE CASCADE,
    credential_ciphertext BYTEA NOT NULL,
    key_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'revoking', 'revoked', 'error')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    UNIQUE (device_id, endpoint_id)
);

CREATE TABLE usage_daily (
    user_id UUID NOT NULL REFERENCES users(id),
    device_id UUID NOT NULL REFERENCES devices(id),
    usage_day DATE NOT NULL,
    bytes_up BIGINT NOT NULL DEFAULT 0 CHECK (bytes_up >= 0),
    bytes_down BIGINT NOT NULL DEFAULT 0 CHECK (bytes_down >= 0),
    PRIMARY KEY (device_id, usage_day)
);

CREATE TABLE admin_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email CITEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('owner', 'operations', 'support')),
    mfa_required BOOLEAN NOT NULL DEFAULT TRUE,
    disabled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE audit_events (
    id BIGSERIAL PRIMARY KEY,
    actor_admin_id UUID REFERENCES admin_accounts(id),
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_created_idx ON audit_events(created_at DESC);

CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_kind TEXT NOT NULL CHECK (actor_kind IN ('user', 'admin')),
    actor_id UUID NOT NULL,
    token_hash BYTEA UNIQUE NOT NULL,
    csrf_token TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);
CREATE INDEX sessions_actor_idx ON sessions(actor_kind, actor_id) WHERE revoked_at IS NULL;
CREATE INDEX sessions_expiry_idx ON sessions(expires_at);
