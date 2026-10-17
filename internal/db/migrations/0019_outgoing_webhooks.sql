-- Outgoing webhooks: a member-configured URL that receives signed events.
--
-- An outgoing webhook belongs to its channel and receives that channel's
-- events only; created_by is audit information. The HMAC secret has to be
-- readable to sign, so it is stored as is; it is shown once. Deliveries
-- are queued in memory (lost on restart, see doc/webhooks.md); this table only
-- keeps a short log per webhook, pruned on insert.

CREATE TABLE outgoing_webhooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    name VARCHAR(80) NOT NULL CHECK (char_length(btrim(name)) > 0),
    url TEXT NOT NULL CHECK (char_length(url) <= 2048),
    secret TEXT NOT NULL,
    events TEXT[] NOT NULL CHECK (cardinality(events) > 0),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    auto_disabled BOOLEAN NOT NULL DEFAULT FALSE,
    consecutive_failures INT NOT NULL DEFAULT 0,
    last_status INT,
    last_error TEXT,
    last_attempt_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX outgoing_webhooks_channel ON outgoing_webhooks(channel_id);

CREATE TABLE outgoing_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id UUID NOT NULL REFERENCES outgoing_webhooks(id) ON DELETE CASCADE,
    event_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    attempt INT NOT NULL,
    success BOOLEAN NOT NULL,
    status_code INT,
    error TEXT,
    duration_ms INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX outgoing_deliveries_webhook ON outgoing_deliveries(webhook_id, created_at DESC);
