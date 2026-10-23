-- Pinned messages: who pinned a message and when, so a channel's pinned list
-- is ordered by pinning (newest first) and the audit trail has the actor.
-- is_pinned stays the flag every message row already carries.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS pinned_at TIMESTAMPTZ;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS pinned_by UUID REFERENCES users(id) ON DELETE SET NULL;

UPDATE messages SET pinned_at = updated_at WHERE is_pinned AND pinned_at IS NULL;

-- The pinned list of one channel reads only these rows.
CREATE INDEX IF NOT EXISTS idx_messages_channel_pinned
    ON messages (channel_id, pinned_at DESC) WHERE is_pinned;
