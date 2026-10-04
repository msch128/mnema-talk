-- Discord-style replies: a message may quote another message of the same
-- channel. Deliberately no foreign key: when the original is deleted the reply
-- keeps its pointer and the client shows "original message was deleted".
ALTER TABLE messages ADD COLUMN IF NOT EXISTS reply_to_id UUID;
CREATE INDEX IF NOT EXISTS idx_messages_reply_to ON messages(reply_to_id) WHERE reply_to_id IS NOT NULL;

-- History paging walks root messages of one channel by (created_at, id) in
-- both directions; this partial index serves before/after/around queries
-- without touching thread replies.
CREATE INDEX IF NOT EXISTS idx_messages_channel_roots
    ON messages(channel_id, created_at DESC, id DESC)
    WHERE parent_id IS NULL;
