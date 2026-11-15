CREATE TABLE payment_orders (
    order_no TEXT PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    plan_id UUID NOT NULL REFERENCES plans(id),
    product TEXT NOT NULL,
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    duration_days INTEGER NOT NULL CHECK (duration_days > 0),
    provider_order_no TEXT UNIQUE,
    subscription_id UUID UNIQUE REFERENCES subscriptions(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at TIMESTAMPTZ
);
CREATE INDEX payment_orders_user_idx ON payment_orders(user_id, created_at DESC);
