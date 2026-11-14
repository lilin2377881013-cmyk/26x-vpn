CREATE TABLE support_tickets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    message TEXT NOT NULL CHECK (char_length(message) BETWEEN 1 AND 2000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    notified_at TIMESTAMPTZ,
    notify_after TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX support_tickets_created_idx ON support_tickets (created_at DESC, id DESC);
CREATE INDEX support_tickets_user_idx ON support_tickets (user_id, created_at DESC);
CREATE INDEX support_tickets_notify_idx ON support_tickets (notify_after) WHERE notified_at IS NULL;
