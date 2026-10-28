-- GetReadStates counts every channel's messages newer than the member's read
-- marker. This covering index lets that count run as an index-only scan
-- (channel, time range; author, thread and reply columns come from the index)
-- instead of fetching every unread message from the table. The migrator runs
-- each file in a transaction, so this cannot be CONCURRENTLY.
CREATE INDEX IF NOT EXISTS messages_channel_unread_idx
    ON messages (channel_id, created_at) INCLUDE (user_id, parent_id, reply_to_id);
