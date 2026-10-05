-- Per user and channel: how far the user has read and how loudly the channel
-- may notify. A missing row means "read up to the user's sign-up" and "all".
CREATE TABLE IF NOT EXISTS channel_reads (
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id   UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    last_read_at TIMESTAMPTZ,
    notify_level VARCHAR(16) NOT NULL DEFAULT 'all'
        CHECK (notify_level IN ('all', 'mentions', 'mute')),
    PRIMARY KEY (user_id, channel_id)
);

-- Unread counting scans a channel's messages newer than a timestamp.
CREATE INDEX IF NOT EXISTS idx_messages_channel_created ON messages(channel_id, created_at);
