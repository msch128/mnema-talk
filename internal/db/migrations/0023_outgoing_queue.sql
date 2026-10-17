-- Outgoing webhook deliveries that are still to be made (or retried).
--
-- Until now the queue lived in memory, so a restart lost every event not yet
-- delivered. This table is the source of truth: the dispatcher inserts an
-- event per receiving webhook, claims due rows with FOR UPDATE SKIP LOCKED
-- under a short lease (a crashed claimer's rows become due again when the
-- lease ends), and deletes a row once the event is settled. The body is
-- rebuilt and signed at every attempt from these columns, so the signature
-- header always carries the time of the attempt actually made.
-- seq is the order of arrival: "oldest" for the caps means lowest seq.

CREATE TABLE outgoing_queue (
    seq BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    webhook_id UUID NOT NULL REFERENCES outgoing_webhooks(id) ON DELETE CASCADE,
    channel_id UUID NOT NULL,
    event_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    event_time TIMESTAMPTZ NOT NULL,
    data BYTEA NOT NULL,
    -- the number of the next attempt (1 = first)
    attempt INT NOT NULL DEFAULT 1 CHECK (attempt >= 1),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX outgoing_queue_due ON outgoing_queue(next_attempt_at, seq);
CREATE INDEX outgoing_queue_webhook ON outgoing_queue(webhook_id, seq);
CREATE INDEX outgoing_queue_created ON outgoing_queue(created_at);
