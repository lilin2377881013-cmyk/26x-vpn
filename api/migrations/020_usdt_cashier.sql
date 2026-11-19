ALTER TABLE payment_orders
    ADD COLUMN payment_channel TEXT NOT NULL DEFAULT 'alipay'
        CHECK (payment_channel IN ('selection', 'alipay', 'usdt')),
    ADD COLUMN checkout_token_hash BYTEA UNIQUE,
    ADD COLUMN checkout_expires_at TIMESTAMPTZ,
    ADD COLUMN cashier_url TEXT;

CREATE TABLE usdt_wallets (
    address TEXT PRIMARY KEY,
    scan_through_ms BIGINT NOT NULL DEFAULT 0,
    last_success_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE usdt_invoices (
    order_no TEXT PRIMARY KEY REFERENCES payment_orders(order_no),
    address TEXT NOT NULL REFERENCES usdt_wallets(address),
    network TEXT NOT NULL DEFAULT 'TRC20' CHECK (network = 'TRC20'),
    token_contract TEXT NOT NULL,
    payment_code INTEGER NOT NULL CHECK (payment_code BETWEEN 1 AND 999999),
    quote_units BIGINT NOT NULL CHECK (quote_units > 0),
    amount_units BIGINT NOT NULL CHECK (amount_units >= quote_units),
    status TEXT NOT NULL DEFAULT 'waiting' CHECK (status IN ('waiting', 'paid', 'expired', 'review')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    CHECK (expires_at > created_at),
    CHECK (amount_units % 1000000 = payment_code),
    CHECK (amount_units - quote_units <= 10000),
    UNIQUE(address, payment_code),
    UNIQUE(address, amount_units)
);

CREATE TABLE usdt_transfers (
    tx_hash TEXT NOT NULL CHECK (tx_hash ~ '^[a-f0-9]{64}$'),
    log_index INTEGER NOT NULL CHECK (log_index >= 0),
    address TEXT NOT NULL REFERENCES usdt_wallets(address),
    amount_units NUMERIC(78,0) NOT NULL CHECK (amount_units > 0),
    block_number BIGINT NOT NULL CHECK (block_number > 0),
    block_time TIMESTAMPTZ NOT NULL,
    order_no TEXT REFERENCES usdt_invoices(order_no),
    result TEXT NOT NULL CHECK (result IN ('paid','review','unmatched')),
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(tx_hash, log_index)
);
CREATE UNIQUE INDEX usdt_transfer_paid_order ON usdt_transfers(order_no) WHERE result='paid';
CREATE INDEX usdt_invoice_pending ON usdt_invoices(address, expires_at) WHERE status='waiting';
CREATE INDEX usdt_transfer_review ON usdt_transfers(created_at DESC) WHERE result <> 'paid';
